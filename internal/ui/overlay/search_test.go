package overlay

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ Overlay                           = (*Search)(nil)
	_ func(theme.Theme, string) *Search = NewSearch
	_ func(*Search) string              = (*Search).Value
	_ tea.Msg                           = SearchMsg{}
)

// --- Behavior tests ---

func searchOverlay(t *testing.T, query string) *Search {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return NewSearch(th, query)
}

// searchMsgs runs cmd and expands a batch into its messages.
func searchMsgs(cmd tea.Cmd) []tea.Msg {
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
		out = append(out, searchMsgs(c)...)
	}
	return out
}

func TestSearchEnterSendsQueryAndCloses(t *testing.T) {
	s := searchOverlay(t, "")
	for _, r := range "q j?" {
		s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if s.Value() != "q j?" {
		t.Fatalf("command keys did not reach the field: %q", s.Value())
	}
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	var gotQuery, closed bool
	for _, msg := range searchMsgs(cmd) {
		switch msg := msg.(type) {
		case SearchMsg:
			gotQuery = msg.Query == "q j?"
		case CloseMsg:
			closed = true
		}
	}
	if !gotQuery || !closed {
		t.Fatalf("enter sent query=%v close=%v", gotQuery, closed)
	}
}

func TestSearchEscOnlyCloses(t *testing.T) {
	s := searchOverlay(t, "old")
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	msgs := searchMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("esc sent %v", msgs)
	}
	if _, ok := msgs[0].(CloseMsg); !ok {
		t.Fatalf("esc sent %T, want CloseMsg", msgs[0])
	}
}

func TestSearchStartsWithQueryAndTakesPaste(t *testing.T) {
	s := searchOverlay(t, "err")
	s.Update(tea.PasteMsg{Content: "or"})
	if s.Value() != "error" {
		t.Fatalf("Value = %q, want error", s.Value())
	}
	if s.Title() != "Search logs" {
		t.Fatalf("Title = %q", s.Title())
	}
}

func TestSearchView(t *testing.T) {
	s := searchOverlay(t, "")
	out := s.View(80, 5)
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "/search logs") || lines[1] != "enter search · esc cancel" {
		t.Fatalf("View = %q", lines)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > searchWidth {
			t.Fatalf("line is %d cells wide, max %d", w, searchWidth)
		}
	}
	if got := strings.Split(s.View(10, 1), "\n"); len(got) != 1 || ansi.StringWidth(got[0]) > 10 {
		t.Fatalf("View(10, 1) = %q", got)
	}
}
