package logview

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

func mono(t testing.TB) theme.Theme {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return th
}

// feed appends one message holding lines for span "a" (resource "api")
// and span "w" (resource "web").
func feed(t testing.TB, s *logstore.Store, complete bool, texts ...string) {
	t.Helper()
	type seg struct {
		SpanID string    `json:"spanId"`
		Time   time.Time `json:"time"`
		Text   string    `json:"text"`
	}
	segs := make([]seg, len(texts))
	for i, tx := range texts {
		span := "a"
		if strings.HasPrefix(tx, "web:") {
			span = "w"
		}
		segs[i] = seg{SpanID: span, Time: time.Date(2026, 11, 3, 12, 0, i%60, 0, time.UTC), Text: tx + "\n"}
	}
	raw, _ := json.Marshal(map[string]any{
		"isComplete": complete,
		"logList": map[string]any{
			"segments": segs,
			"spans":    map[string]any{"a": map[string]string{"manifestName": "api"}, "w": map[string]string{"manifestName": "web"}},
		},
	})
	var v tilt.View
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	s.Append(v)
}

func numbered(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

func key(s string) tea.KeyPressMsg {
	codes := map[string]rune{"up": tea.KeyUp, "down": tea.KeyDown, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd}
	if c, ok := codes[s]; ok {
		return tea.KeyPressMsg{Code: c}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// rows returns the text area of each row: escapes stripped, scrollbar
// column removed, trailing spaces trimmed.
func rows(m *Model, w, h int) []string {
	lines := strings.Split(m.View(w, h, true), "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		p := []rune(ansi.Strip(l))
		if len(p) > 0 {
			p = p[:len(p)-1]
		}
		out[i] = strings.TrimRight(string(p), " ")
	}
	return out
}

func want(t *testing.T, got []string, exp ...string) {
	t.Helper()
	if !slices.Equal(got, exp) {
		t.Fatalf("rows = %q, want %q", got, exp)
	}
}

func setup(t *testing.T) (*logstore.Store, *Model) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, numbered(1, 100)...)
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	return s, m
}

func TestFollowShowsNewest(t *testing.T) {
	_, m := setup(t)
	want(t, rows(m, 40, 5), numbered(96, 100)...)
	if !m.Following() {
		t.Fatal("not following at start")
	}
}

func TestScrollUpStopsFollowAndHoldsPosition(t *testing.T) {
	s, m := setup(t)
	rows(m, 40, 5)
	m.HandleKey(key("k"))
	if m.Following() {
		t.Fatal("still following after k")
	}
	want(t, rows(m, 40, 5), numbered(95, 99)...)
	feed(t, s, false, "line 101")
	want(t, rows(m, 40, 5), numbered(95, 99)...)
	m.HandleKey(key("G"))
	if !m.Following() {
		t.Fatal("G did not turn follow on")
	}
	want(t, rows(m, 40, 5), numbered(97, 101)...)
}

func TestSTogglesFollowInPlace(t *testing.T) {
	s, m := setup(t)
	rows(m, 40, 5)
	m.HandleKey(key("s"))
	if m.Following() {
		t.Fatal("s did not turn follow off")
	}
	feed(t, s, false, "line 101")
	want(t, rows(m, 40, 5), numbered(96, 100)...)
	m.HandleKey(key("s"))
	want(t, rows(m, 40, 5), numbered(97, 101)...)
}

func TestTopAndPaging(t *testing.T) {
	_, m := setup(t)
	rows(m, 40, 5)
	m.HandleKey(key("g"))
	want(t, rows(m, 40, 5), numbered(1, 5)...)
	m.HandleKey(key("pgdown"))
	want(t, rows(m, 40, 5), numbered(5, 9)...)
	for range 50 {
		m.HandleKey(key("pgdown"))
	}
	want(t, rows(m, 40, 5), numbered(96, 100)...)
	if m.Following() {
		t.Fatal("reaching the bottom turned follow on")
	}
	m.HandleKey(key("pgup"))
	want(t, rows(m, 40, 5), numbered(92, 96)...)
}

func TestMouseWheel(t *testing.T) {
	_, m := setup(t)
	rows(m, 40, 5)
	m.HandleMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	want(t, rows(m, 40, 5), numbered(93, 97)...)
	m.HandleMouse(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	want(t, rows(m, 40, 5), numbered(96, 100)...)
}

func TestScrollThroughWrappedRows(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "short", strings.Repeat("x", 25), "end")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "")
	// text area is 10 cells: the long line takes 3 rows.
	want(t, rows(m, 11, 4), "xxxxxxxxxx", "↳ xxxxxxxx", "↳ xxxxxxx", "end")
	m.HandleKey(key("k"))
	want(t, rows(m, 11, 4), "short", "xxxxxxxxxx", "↳ xxxxxxxx", "↳ xxxxxxx")
	m.HandleKey(key("j"))
	m.HandleKey(key("j")) // already at the bottom: no further scroll
	want(t, rows(m, 11, 4), "xxxxxxxxxx", "↳ xxxxxxxx", "↳ xxxxxxx", "end")
}

func TestSourceFilters(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "api 1", "web: 1", "api 2")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{Resource: "api"}, "api")
	want(t, rows(m, 20, 2), "api 1", "api 2")
	m.SetSource(logstore.Source{All: true}, "All Resources")
	want(t, rows(m, 20, 3), "api 1", "web: 1", "api 2")
	if m.Title() != "All Resources" {
		t.Fatalf("Title = %q", m.Title())
	}
}

func TestEveryRowEndsWithReset(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "\x1b[31mred never closed", "plain")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "")
	for i, l := range strings.Split(m.View(30, 2, true), "\n") {
		if !strings.Contains(l, "\x1b[0m") {
			t.Fatalf("row %d lacks an SGR reset: %q", i, l)
		}
	}
}

func TestResetClearsWrapCache(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "old text")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "")
	want(t, rows(m, 20, 1), "old text")
	m.HandleKey(key("k"))
	feed(t, s, true, "new text") // same Seq 1, Version 0, same width
	want(t, rows(m, 20, 1), "new text")
	if !m.Following() {
		t.Fatal("reset did not switch to follow mode")
	}
}

func TestTimestamps(t *testing.T) {
	s := logstore.New(logstore.Options{})
	feed(t, s, true, "hello")
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "")
	m.HandleKey(key("t"))
	ts := s.Prev(logstore.Source{All: true}, logstore.End, 1)[0].Time.Local().Format("15:04:05")
	want(t, rows(m, 30, 1), ts+" hello")
	if !m.Timestamps() {
		t.Fatal("Timestamps() false after t")
	}
}

func TestEmptyAndSize(t *testing.T) {
	s := logstore.New(logstore.Options{})
	m := New(s, mono(t))
	m.SetSource(logstore.Source{Resource: "api"}, "api")
	got := rows(m, 20, 3)
	want(t, got, "no logs yet", "", "")
	feed(t, s, true, strings.Repeat("日本", 40))
	for i, l := range strings.Split(m.View(20, 3, false), "\n") {
		if w := ansi.StringWidth(l); w > 20 {
			t.Fatalf("row %d is %d cells", i, w)
		}
	}
}

func TestUnhandledKey(t *testing.T) {
	_, m := setup(t)
	if m.HandleKey(key("r")) {
		t.Fatal("logview claimed the r key")
	}
}

func TestWrapCacheIsCapped(t *testing.T) {
	m := New(logstore.New(logstore.Options{}), mono(t))
	for i := range 20001 {
		m.rowsOf(logstore.Line{Seq: logstore.Seq(i + 1), Text: "x"})
	}
	if n := len(m.cache); n > 20000 {
		t.Fatalf("cache holds %d entries", n)
	}
}

// bar returns the scrollbar column of each row.
func bar(m *Model, w, h int) string {
	var b strings.Builder
	for _, l := range strings.Split(ansi.Strip(m.View(w, h, true)), "\n") {
		p := []rune(l)
		b.WriteRune(p[len(p)-1])
	}
	return b.String()
}

func TestScrollbar(t *testing.T) {
	s := logstore.New(logstore.Options{})
	m := New(s, mono(t))
	m.SetSource(logstore.Source{All: true}, "")
	if got := bar(m, 20, 4); got != "││││" {
		t.Fatalf("empty bar = %q", got)
	}
	feed(t, s, true, numbered(1, 8)...)
	if got := bar(m, 20, 4); got != "││┃┃" {
		t.Fatalf("following bar = %q", got)
	}
	m.HandleKey(key("g"))
	if got := bar(m, 20, 4); got != "┃┃││" {
		t.Fatalf("top bar = %q", got)
	}
}
