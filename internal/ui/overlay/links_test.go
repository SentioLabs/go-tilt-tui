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
	_ Overlay                                                                     = (*Links)(nil)
	_ func(theme.Theme, string, []tilt.EndpointLink, func(string) tea.Cmd) *Links = NewLinks
)

// --- Behavior tests ---

// linksOpenMsg is what the test opener's command returns.
type linksOpenMsg string

func linksOpen(url string) tea.Cmd { return func() tea.Msg { return linksOpenMsg(url) } }

func linksMenu(t *testing.T) *Links {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return NewLinks(th, "api", []tilt.EndpointLink{
		{URL: "http://localhost:8080", Name: "web"},
		{URL: "http://localhost:9090/metrics"},
		{URL: "https://localhost:8443", Name: "admin"},
	}, linksOpen)
}

// linksMsgs runs cmd and expands a batch into its messages.
func linksMsgs(cmd tea.Cmd) []tea.Msg {
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
		out = append(out, linksMsgs(c)...)
	}
	return out
}

// linksOpened returns the URL the opener got, and whether a CloseMsg
// came with it.
func linksOpened(cmd tea.Cmd) (url string, closed bool) {
	for _, msg := range linksMsgs(cmd) {
		switch msg := msg.(type) {
		case linksOpenMsg:
			url = string(msg)
		case CloseMsg:
			closed = true
		}
	}
	return url, closed
}

func TestLinksView(t *testing.T) {
	l := linksMenu(t)
	got := strings.Split(ansi.Strip(l.View(80, 10)), "\n")
	for i := range got {
		got[i] = strings.TrimRight(got[i], " ")
	}
	want := []string{"1 web  http://localhost:8080", "2 http://localhost:9090/metrics", "3 admin  https://localhost:8443"}
	if !slices.Equal(got, want) {
		t.Fatalf("View = %q, want %q", got, want)
	}
	if l.Title() != "Open link · api" {
		t.Fatalf("Title = %q", l.Title())
	}
}

func TestLinksEnterOpensCursor(t *testing.T) {
	l := linksMenu(t)
	for _, k := range []tea.KeyPressMsg{{Code: 'j', Text: "j"}, {Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: 'k', Text: "k"}} {
		l.Update(k)
	}
	_, cmd := l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	url, closed := linksOpened(cmd)
	if url != "http://localhost:9090/metrics" || !closed {
		t.Fatalf("enter opened %q, closed %v", url, closed)
	}
}

func TestLinksDigitOpens(t *testing.T) {
	l := linksMenu(t)
	_, cmd := l.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	if url, closed := linksOpened(cmd); url != "https://localhost:8443" || !closed {
		t.Fatalf("3 opened %q, closed %v", url, closed)
	}
	if _, cmd := l.Update(tea.KeyPressMsg{Code: '7', Text: "7"}); cmd != nil {
		t.Fatal("a digit past the last link did something")
	}
}

func TestLinksEscCloses(t *testing.T) {
	l := linksMenu(t)
	_, cmd := l.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if url, closed := linksOpened(cmd); url != "" || !closed {
		t.Fatalf("esc opened %q, closed %v", url, closed)
	}
}

var linksSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestLinksTextPassesThroughTermsafe(t *testing.T) {
	th, _ := theme.ByName("mono")
	raw := "http://x\x1b]52;c;ZXZpbA==\x07y"
	l := NewLinks(th, "a\x1b]0;t\x07pi", []tilt.EndpointLink{{URL: raw, Name: "n\x1b[2Jame"}}, linksOpen)
	out := l.View(80, 5) + l.Title()
	if rest := linksSGR.ReplaceAllString(out, ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("non-SGR escape in view: %q", rest)
	}
	if !strings.Contains(ansi.Strip(out), "http://xy") || !strings.Contains(l.Title(), "api") {
		t.Fatalf("view = %q", ansi.Strip(out))
	}
	_, cmd := l.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if url, _ := linksOpened(cmd); url != raw {
		t.Fatalf("opened %q, want the raw URL for the opener to check", url)
	}
}

func TestLinksViewFits(t *testing.T) {
	l := linksMenu(t)
	for _, w := range []int{10, 30, 200} {
		for _, line := range strings.Split(l.View(w, 2), "\n") {
			if lw := ansi.StringWidth(line); lw > min(w, linksWidth) {
				t.Fatalf("View(%d) line is %d wide", w, lw)
			}
		}
	}
	if n := len(strings.Split(l.View(80, 2), "\n")); n != 2 {
		t.Fatalf("View(80, 2) has %d lines", n)
	}
}
