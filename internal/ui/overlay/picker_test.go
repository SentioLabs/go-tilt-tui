package overlay

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ Overlay                                    = (*Picker)(nil)
	_ func(theme.Theme, []tilt.Resource) *Picker = NewPicker
	_ func(*Picker) string                       = (*Picker).Value
	_ func(*Picker) (string, bool)               = (*Picker).Selected
	_ tea.Msg                                    = PickMsg{}
)

// --- Behavior tests ---

func pickRes(name, app string) tilt.Resource {
	r := tilt.Resource{Metadata: tilt.ObjectMeta{Name: name}}
	if app != "" {
		r.Metadata.Labels = map[string]string{"app": app}
	}
	r.Status.UpdateStatus = tilt.UpdateOK
	return r
}

func picker(t *testing.T, rs ...tilt.Resource) *Picker {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return NewPicker(th, rs)
}

func pickType(p *Picker, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// pickLines is the view without SGR, one entry per row, trailing spaces
// trimmed.
func pickLines(p *Picker, h int) []string {
	lines := strings.Split(ansi.Strip(p.View(80, h)), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// pickMsgs runs cmd and expands a batch into its messages.
func pickMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	b, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range b {
		out = append(out, pickMsgs(c)...)
	}
	return out
}

func pickSample() []tilt.Resource {
	return []tilt.Resource{
		pickRes("(Tiltfile)", ""),
		pickRes("web-api", "frontend"),
		pickRes("api-gateway", "backend"),
		pickRes("api", "backend"),
		pickRes("db", "backend"),
	}
}

func TestPickerEmptyQueryGroupsLikeTheList(t *testing.T) {
	p := picker(t, pickSample()...)
	want := []string{
		"› resource name",
		"backend",
		"  ● api-gateway",
		"  ● api",
		"  ● db",
		"frontend",
		"  ● web-api",
		"ungrouped",
		"  ● (Tiltfile)",
	}
	if got := pickLines(p, 20); !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func TestPickerRanksExactThenPrefixThenFuzzy(t *testing.T) {
	p := picker(t, pickSample()...)
	pickType(p, "api")
	want := []string{"› api", "backend", "  ● api", "  ● api-gateway", "frontend", "  ● web-api"}
	if got := pickLines(p, 20); !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if name, ok := p.Selected(); !ok || name != "api" {
		t.Fatalf("Selected = %q %v, want the exact match", name, ok)
	}
}

func TestPickerOrdersGroupsByBestMatch(t *testing.T) {
	p := picker(t, pickRes("my-web", "a-group"), pickRes("web", "z-group"))
	pickType(p, "web")
	want := []string{"› web", "z-group", "  ● web", "a-group", "  ● my-web"}
	if got := pickLines(p, 20); !slices.Equal(got, want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func TestPickerEnterPicksCursor(t *testing.T) {
	p := picker(t, pickSample()...)
	pickType(p, "api")
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // stops at the last match
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var picked string
	closed := false
	for _, msg := range pickMsgs(cmd) {
		switch msg := msg.(type) {
		case PickMsg:
			picked = msg.Name
		case CloseMsg:
			closed = true
		}
	}
	if picked != "api-gateway" || !closed {
		t.Fatalf("enter picked %q, closed %v", picked, closed)
	}
}

func TestPickerNoMatch(t *testing.T) {
	p := picker(t, pickSample()...)
	pickType(p, "zzz")
	if got := pickLines(p, 5); !slices.Equal(got, []string{"› zzz", "no match"}) {
		t.Fatalf("rows = %q", got)
	}
	if _, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter with no match sent a message")
	}
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if msgs := pickMsgs(cmd); len(msgs) != 1 {
		t.Fatalf("esc sent %v", msgs)
	} else if _, ok := msgs[0].(CloseMsg); !ok {
		t.Fatalf("esc sent %T", msgs[0])
	}
}

func TestPickerKeysAndPasteEditQuery(t *testing.T) {
	p := picker(t, pickSample()...)
	pickType(p, "j q")
	p.Update(tea.PasteMsg{Content: "?"})
	if p.Value() != "j q?" {
		t.Fatalf("Value = %q", p.Value())
	}
	if p.Title() != "Go to resource" {
		t.Fatalf("Title = %q", p.Title())
	}
}

var pickSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestPickerNamesPassThroughTermsafe(t *testing.T) {
	evil := "x\x1b]52;c;ZXZpbA==\x07evil"
	p := picker(t, pickRes(evil, "g\x1b]0;t\x07rp"))
	out := p.View(80, 5)
	if rest := pickSGR.ReplaceAllString(out, ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("non-SGR escape in view: %q", rest)
	}
	if !strings.Contains(ansi.Strip(out), "xevil") || !strings.Contains(ansi.Strip(out), "grp") {
		t.Fatalf("view = %q", ansi.Strip(out))
	}
	pickType(p, "xev")
	if name, _ := p.Selected(); name != evil {
		t.Fatalf("Selected = %q, want the raw name for the store", name)
	}
}

func TestPickerViewFitsAndScrolls(t *testing.T) {
	var rs []tilt.Resource
	for _, n := range []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8"} {
		rs = append(rs, pickRes(n+"-a-very-long-resource-name-that-will-not-fit-in-the-picker", ""))
	}
	p := picker(t, rs...)
	for range 7 {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	out := p.View(80, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("View(80, 4) has %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > pickerWidth {
			t.Fatalf("line %d is %d cells wide", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(lines[3]), "a8") {
		t.Fatalf("cursor row not in view:\n%s", ansi.Strip(out))
	}
}
