package overlay

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ Overlay                                       = (*Palette)(nil)
	_ func(theme.Theme, []command.Command) *Palette = NewPalette
	_ func(*Palette) string                         = (*Palette).Title
	_ func(*Palette, tea.Msg) (Overlay, tea.Cmd)    = (*Palette).Update
	_ func(*Palette, int, int) string               = (*Palette).View
	_ tea.Msg                                       = PaletteRunMsg{}
)

// --- Behavior tests ---

func paletteNew(t *testing.T) *Palette {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	noop := func(command.Context) tea.Cmd { return nil }
	return NewPalette(th, []command.Command{
		{ID: "help", Title: "help", Keys: []string{"?"}, Run: noop},
		{ID: "trigger", Title: "trigger", Keys: []string{"r"}, Run: noop},
		{ID: "quit", Title: "quit", Keys: []string{"q"}, Run: noop},
	})
}

func paletteType(p *Palette, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// paletteRows is the view without SGR and the field line, trailing
// spaces trimmed.
func paletteRows(p *Palette, h int) []string {
	lines := strings.Split(ansi.Strip(p.View(80, h)), "\n")[1:]
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// paletteTitles is the first word of every result row.
func paletteTitles(p *Palette) []string {
	var out []string
	for _, l := range paletteRows(p, 20) {
		out = append(out, strings.Fields(l)[0])
	}
	return out
}

func paletteMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	return cmd()
}

func TestPaletteEmptyQueryListsAllInOrder(t *testing.T) {
	p := paletteNew(t)
	if got := paletteTitles(p); !slices.Equal(got, []string{"help", "trigger", "quit"}) {
		t.Fatalf("titles = %q", got)
	}
	if p.Title() != "Commands" {
		t.Fatalf("Title = %q", p.Title())
	}
}

func TestPaletteRowShowsKeysOnTheRight(t *testing.T) {
	p := paletteNew(t)
	rows := paletteRows(p, 20)
	if !strings.HasSuffix(rows[1], " r") || len(strings.Fields(rows[1])) != 2 {
		t.Fatalf("trigger row = %q", rows[1])
	}
	if w := ansi.StringWidth(strings.Split(ansi.Strip(p.View(80, 20)), "\n")[1]); w != 56 {
		t.Fatalf("highlighted row is %d cells, want 56", w)
	}
}

func TestPaletteQueryRanks(t *testing.T) {
	p := paletteNew(t)
	paletteType(p, "tr")
	if got := paletteTitles(p); len(got) == 0 || got[0] != "trigger" {
		t.Fatalf("titles = %q, want trigger first", got)
	}
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg := paletteMsg(t, cmd); msg != (PaletteRunMsg{ID: "trigger"}) {
		t.Fatalf("enter sent %#v", msg)
	}
}

func TestPaletteNoMatch(t *testing.T) {
	p := paletteNew(t)
	paletteType(p, "zzz")
	if got := paletteRows(p, 5); !slices.Equal(got, []string{"no match"}) {
		t.Fatalf("rows = %q", got)
	}
	if _, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with no match sent a message")
	}
}

func TestPaletteEscCloses(t *testing.T) {
	p := paletteNew(t)
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if msg := paletteMsg(t, cmd); msg != (CloseMsg{}) {
		t.Fatalf("esc sent %#v", msg)
	}
}

func TestPaletteDownPicksSecond(t *testing.T) {
	p := paletteNew(t)
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg := paletteMsg(t, cmd); msg != (PaletteRunMsg{ID: "trigger"}) {
		t.Fatalf("down, enter sent %#v", msg)
	}
	p.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	_, cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if msg := paletteMsg(t, cmd); msg != (PaletteRunMsg{ID: "help"}) {
		t.Fatalf("ctrl+p, enter sent %#v", msg)
	}
}

func TestPaletteLettersAndPasteEditQuery(t *testing.T) {
	p := paletteNew(t)
	paletteType(p, "jk")
	p.Update(tea.PasteMsg{Content: "x"})
	if p.field.Value() != "jkx" {
		t.Fatalf("query = %q", p.field.Value())
	}
	if first := strings.Split(ansi.Strip(p.View(80, 5)), "\n")[0]; !strings.Contains(first, "jkx") {
		t.Fatalf("field = %q", first)
	}
}

func TestPaletteViewFitsAndScrolls(t *testing.T) {
	p := paletteNew(t)
	out := p.View(80, 3)
	lines := strings.Split(out, "\n")
	if len(lines) > 3 {
		t.Fatalf("View(80, 3) has %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 56 {
			t.Fatalf("line %d is %d cells wide", i, w)
		}
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if rows := paletteRows(p, 3); len(rows) != 2 || !strings.HasPrefix(strings.TrimSpace(rows[1]), "quit") {
		t.Fatalf("cursor row not in view: %q", rows)
	}
}
