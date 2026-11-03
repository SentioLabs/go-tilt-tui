package logview

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
)

// --- Contract assertions ---

var (
	_ func(*Model, string)                              = (*Model).SetQuery
	_ func(*Model) string                               = (*Model).Query
	_ func(*Model) (int, int, bool)                     = (*Model).MatchPos
	_ func(*Model, []logstore.Source) bool              = (*Model).NextMatch
	_ func(*Model, []logstore.Source) bool              = (*Model).PrevMatch
	_ func(logstore.Source, []string) []logstore.Source = Parts
)

// --- Behavior tests ---

const testOn = "\x1b[7m"

// names are the resources feed writes to.
var names = []string{"api", "web"}

func TestMatchSmartCase(t *testing.T) {
	tests := []struct {
		q, text string
		want    bool
	}{
		{"err", "ERROR: disk full", true},
		{"err", "an error", true},
		{"Err", "an error", false},
		{"Err", "Err 42", true},
		{"redtext", "\x1b[31mred\x1b[0mtext", true},
		{"31m", "\x1b[31mred", false},
		{"日本", "abc日本語", true},
		{"日本", "plain ascii", false},
		{"straße", "STRASSE straße", true},
		{"", "anything", false},
	}
	for _, tt := range tests {
		if got := newMatcher(tt.q).match(tt.text); got != tt.want {
			t.Errorf("match(%q, %q) = %v, want %v", tt.q, tt.text, got, tt.want)
		}
	}
}

func TestSpansCoverWholeGraphemes(t *testing.T) {
	tests := []struct {
		q, text string
		want    []span
	}{
		{"b", "abcb", []span{{1, 2}, {3, 4}}},
		{"aa", "aaaa", []span{{0, 4}}},
		{"語テ", "日本語テキスト", []span{{6, 12}}},
		{"e", "café!", []span{{3, 6}}},
		{"bc", "\x1b[31mab\x1b[1mcd", []span{{6, 12}}},
	}
	for _, tt := range tests {
		if got := newMatcher(tt.q).spans(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("spans(%q, %q) = %v, want %v", tt.q, tt.text, got, tt.want)
		}
	}
}

func TestHighlightKeepsLineSGR(t *testing.T) {
	text := "\x1b[31mab\x1b[1mcd\x1b[0mef"
	got := highlight(text, newMatcher("bcd").spans(text), testOn)
	want := "\x1b[31ma" + testOn + "bcd\x1b[0m\x1b[31m\x1b[1m\x1b[0mef"
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
	if ansi.Strip(got) != ansi.Strip(text) {
		t.Fatalf("highlight changed the text: %q", ansi.Strip(got))
	}
}

func TestHighlightAtEnds(t *testing.T) {
	text := "abcab"
	got := highlight(text, newMatcher("ab").spans(text), testOn)
	want := testOn + "ab\x1b[0mc" + testOn + "ab\x1b[0m"
	if got != want {
		t.Fatalf("highlight = %q, want %q", got, want)
	}
}

// searchStore holds lines "line 1" to "line 20". Lines 5, 12 and 18
// also say "hit".
func searchStore(t *testing.T) (*logstore.Store, *Model) {
	t.Helper()
	s := logstore.New(logstore.Options{})
	var texts []string
	for i := 1; i <= 20; i++ {
		tx := fmt.Sprintf("line %d", i)
		if i == 5 || i == 12 || i == 18 {
			tx += " hit"
		}
		texts = append(texts, tx)
	}
	feed(t, s, true, texts...)
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	return s, m
}

func wantMatch(t *testing.T, m *Model, top string, pos, total int) {
	t.Helper()
	if got := rows(m, 40, 5)[0]; got != top {
		t.Fatalf("top row = %q, want %q", got, top)
	}
	p, n, ok := m.MatchPos()
	if !ok || p != pos || n != total {
		t.Fatalf("MatchPos = %d/%d %v, want %d/%d", p, n, ok, pos, total)
	}
}

func TestNextAndPrevWalkFromCurrentLine(t *testing.T) {
	_, m := searchStore(t)
	rows(m, 40, 5) // follow mode: lines 16 to 20 on screen
	parts := Parts(m.Source(), names)
	m.SetQuery("hit")
	if !m.NextMatch(parts) {
		t.Fatal("NextMatch found nothing")
	}
	if m.Following() {
		t.Fatal("still following after a jump")
	}
	wantMatch(t, m, "line 18 hit", 3, 3)
	m.NextMatch(parts)
	wantMatch(t, m, "line 5 hit", 1, 3) // wrapped to the oldest
	m.NextMatch(parts)
	wantMatch(t, m, "line 12 hit", 2, 3)
	m.PrevMatch(parts)
	wantMatch(t, m, "line 5 hit", 1, 3)
	m.PrevMatch(parts)
	wantMatch(t, m, "line 18 hit", 3, 3) // wrapped to the newest
}

func TestNewSearchCanMatchTopLine(t *testing.T) {
	_, m := searchStore(t)
	rows(m, 40, 5)
	m.HandleKey(key("g")) // oldest line at the top
	rows(m, 40, 5)
	m.SetQuery("line 1")
	m.NextMatch(Parts(m.Source(), names))
	wantMatch(t, m, "line 1", 1, 11)
}

func TestNoMatchLeavesView(t *testing.T) {
	_, m := searchStore(t)
	before := rows(m, 40, 5)
	m.SetQuery("absent")
	if m.NextMatch(Parts(m.Source(), names)) || m.PrevMatch(Parts(m.Source(), names)) {
		t.Fatal("found a match for an absent query")
	}
	if !m.Following() || !slices.Equal(rows(m, 40, 5), before) {
		t.Fatal("a failed search moved the view")
	}
	if _, _, ok := m.MatchPos(); ok {
		t.Fatal("MatchPos ok without a match")
	}
}

func TestSearchAllReadsEveryPart(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "api one", "web: two needle", "api three")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	rows(m, 40, 5)
	m.HandleKey(key("g"))
	m.SetQuery("needle")
	if !m.NextMatch(Parts(m.Source(), names)) {
		t.Fatal("All search missed the web line")
	}
	wantMatch(t, m, "web: two needle", 1, 1)
	if got := Parts(logstore.Source{Resource: "api"}, []string{"api", "web"}); !slices.Equal(got, []logstore.Source{{Resource: "api"}}) {
		t.Fatalf("Parts(api) = %v", got)
	}
}

func TestSearchNeverChangesStore(t *testing.T) {
	s, m := searchStore(t)
	gen := s.Generation()
	_, total := s.Rank(logstore.Source{All: true}, logstore.End)
	m.SetQuery("hit")
	m.NextMatch(Parts(m.Source(), names))
	m.PrevMatch(Parts(m.Source(), names))
	rows(m, 40, 5)
	_, after := s.Rank(logstore.Source{All: true}, logstore.End)
	if s.Generation() != gen || after != total {
		t.Fatal("search changed the store")
	}
}

func TestHighlightDrawsMatches(t *testing.T) {
	_, m := searchStore(t)
	rows(m, 40, 5)
	m.SetQuery("hit")
	view := m.View(40, 5, true)
	on := sgrOpen(mono(t).Match.Render)
	if on == "" || strings.Count(view, on) != 1 {
		t.Fatalf("want one highlight on screen, view:\n%q", view)
	}
	m.SetQuery("")
	if strings.Contains(m.View(40, 5, true), on) {
		t.Fatal("highlight stayed after the query was cleared")
	}
}

func TestHighlightKeepsWrapAndWidth(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true,
		"日本語テキストの\x1b[32mログ行です\x1b[0mおわり",
		"é́ combined é́ marks \x1b[1mbold match\x1b[0m tail text",
	)
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	const w, h = 13, 12
	plain := rows(m, w, h)
	for _, q := range []string{"ストのロ", "グ行", "é", "d mat", "わり"} {
		m.SetQuery(q)
		raw := m.View(w, h, true)
		for i, l := range strings.Split(raw, "\n") {
			if lw := ansi.StringWidth(l); lw > w {
				t.Fatalf("query %q: row %d is %d cells wide", q, i, lw)
			}
		}
		if got := rows(m, w, h); !slices.Equal(got, plain) {
			t.Fatalf("query %q changed the rows:\n%q\nwant\n%q", q, got, plain)
		}
		if !strings.Contains(raw, sgrOpen(mono(t).Match.Render)) {
			t.Fatalf("query %q drew no highlight", q)
		}
	}
}

func TestSGRInsideMatchKeepsHighlight(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "pre \x1b[31mred\x1b[0m post")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.SetQuery("e red p")
	on := sgrOpen(mono(t).Match.Render)
	row := strings.Split(m.View(40, 1, true), "\n")[0]
	want := "pr" + on + "e red p\x1b[0mos"
	if !strings.HasPrefix(row, want) {
		t.Fatalf("row = %q, want prefix %q", row, want)
	}
}

func TestSourceChangeForgetsMatch(t *testing.T) {
	_, m := searchStore(t)
	rows(m, 40, 5)
	m.SetQuery("hit")
	m.NextMatch(Parts(m.Source(), names))
	m.SetSource(logstore.Source{Resource: "api"}, "api")
	if _, _, ok := m.MatchPos(); ok {
		t.Fatal("match kept across a source change")
	}
	if m.Query() != "hit" {
		t.Fatal("query lost across a source change")
	}
}

func TestJumpAfterResetForgetsOldSeq(t *testing.T) {
	s, m := searchStore(t)
	rows(m, 40, 5)
	m.HandleKey(key("g"))
	rows(m, 40, 5)
	m.SetQuery("hit")
	m.NextMatch(Parts(m.Source(), names)) // "line 5 hit", Seq 5
	// The reset restarts Seq at 1. Seq 5 now names "new 5", so a search
	// that kept the old match would land on "new 6 hit".
	feed(t, s, true, "new 1 hit", "new 2", "new 3", "new 4", "new 5", "new 6 hit")
	if !m.NextMatch(Parts(m.Source(), names)) {
		t.Fatal("no match after the reset")
	}
	wantMatch(t, m, "new 1 hit", 1, 2)
}
