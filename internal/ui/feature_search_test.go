package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// searchLogs is a ViewMsg that appends lines to api's log.
func searchLogs(t *testing.T, lines ...string) ViewMsg {
	t.Helper()
	segs := make([]map[string]any, len(lines))
	for i, l := range lines {
		segs[i] = map[string]any{"spanId": "a", "time": now, "text": l + "\n"}
	}
	raw, err := json.Marshal(map[string]any{"logList": map[string]any{
		"spans":    map[string]any{"a": map[string]string{"manifestName": "api"}},
		"segments": segs,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var v tilt.View
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return ViewMsg(v)
}

// searchSetup is setup with only the search feature and 40 more api
// lines. Lines 7 and 31 say "boom".
func searchSetup(t *testing.T, w, h int) Model {
	t.Helper()
	withFeatures(t, searchFeature)
	m, _ := setup(t, w, h, Deps{})
	var lines []string
	for i := 1; i <= 40; i++ {
		l := fmt.Sprintf("api line %d", i)
		if i == 7 || i == 31 {
			l += " boom"
		}
		lines = append(lines, l)
	}
	m, _ = send(m, searchLogs(t, lines...))
	// The first frame applies the snapshot's log reset, which turns
	// follow mode on. The program draws it before any key arrives.
	m.render()
	return m
}

// searchType opens the overlay, types q and presses enter.
func searchType(m Model, q string) Model {
	m = press(m, "/")
	for _, r := range q {
		m = press(m, string(r))
	}
	return press(m, "enter")
}

// searchTop is the first log row on screen, from the wide layout.
func searchTop(t *testing.T, m Model) string {
	t.Helper()
	lines := strings.Split(screen(m), "\n")
	_, right, ok := strings.Cut(lines[1], "││")
	if !ok {
		t.Fatalf("no log pane in:\n%s", screen(m))
	}
	return strings.TrimSpace(strings.TrimRight(right, "│┃ "))
}

func TestSearchJumpsToFirstMatch(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = press(m, "tab", "g", "tab") // oldest log line at the top, list focused
	m = searchType(m, "boom")
	if len(m.overlays) != 0 {
		t.Fatal("enter did not close the search overlay")
	}
	if m.focus != command.FocusLogs {
		t.Fatal("search did not focus the log pane")
	}
	if got := searchTop(t, m); got != "api line 7 boom" {
		t.Fatalf("top log row = %q", got)
	}
	if s := screen(m); !strings.Contains(s, "/boom 1/2") {
		t.Fatalf("log title lacks the match position:\n%s", s)
	}
}

func TestSearchNextAndPrevious(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = press(m, "tab", "g")
	m = searchType(m, "boom")
	m = press(m, "n")
	if got := searchTop(t, m); got != "api line 31 boom" || !strings.Contains(screen(m), "/boom 2/2") {
		t.Fatalf("n: top = %q\n%s", got, screen(m))
	}
	m = press(m, "N")
	if got := searchTop(t, m); got != "api line 7 boom" || !strings.Contains(screen(m), "/boom 1/2") {
		t.Fatalf("N: top = %q\n%s", got, screen(m))
	}
}

func TestSearchCapitalMakesItCaseSensitive(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = searchType(m, "BOOM")
	if !strings.Contains(screen(m), "no match for BOOM") {
		t.Fatalf("a capital letter did not make the search case-sensitive:\n%s", screen(m))
	}
	if _, _, ok := m.logs.MatchPos(); ok {
		t.Fatal("BOOM matched lowercase text")
	}
}

func TestSearchOverlayEditsLastQuery(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = searchType(m, "boo")
	m = press(m, "/")
	if s := screen(m); !strings.Contains(s, "/boo") || !strings.Contains(s, "Search logs") {
		t.Fatalf("overlay does not show the last query:\n%s", s)
	}
	m, _ = send(m, tea.PasteMsg{Content: "m"})
	m = press(m, "q", "esc")
	if len(m.overlays) != 0 {
		t.Fatal("esc did not close the overlay")
	}
	if m.logs.Query() != "boo" {
		t.Fatalf("esc in the overlay changed the query to %q", m.logs.Query())
	}
}

func TestSearchEscClearsQueryInLogs(t *testing.T) {
	m := searchSetup(t, 80, 20)
	m = press(m, "enter") // narrow: open the log pane
	m = searchType(m, "boom")
	m = press(m, "esc")
	if m.logs.Query() != "" {
		t.Fatal("esc did not clear the query")
	}
	if m.focus != command.FocusLogs {
		t.Fatal("the esc that cleared the query also left the log pane")
	}
	if strings.Contains(screen(m), "/boom") {
		t.Fatalf("title still shows the query:\n%s", screen(m))
	}
	m = press(m, "esc")
	if m.focus != command.FocusList {
		t.Fatal("a second esc did not go back to the list")
	}
}

func TestSearchNSkippedWithoutQuery(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = press(m, "tab", "n", "N")
	if strings.Contains(screen(m), "no match") {
		t.Fatalf("n searched without a query:\n%s", screen(m))
	}
}

func TestSearchFooterHintAndHelp(t *testing.T) {
	m := searchSetup(t, 140, 30)
	if s := screen(m); !strings.Contains(s, "r trigger · / search · tab switch") {
		t.Fatalf("footer lacks the search hint:\n%s", s)
	}
	m = searchType(m, "boom")
	m = press(m, "?")
	for _, want := range []string{"search logs", "next match", "previous match", "clear search"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("help lacks %q:\n%s", want, screen(m))
		}
	}
}

func TestSearchOnlySGRReachesTheTerminal(t *testing.T) {
	m := searchSetup(t, 140, 20)
	m = press(m, "/")
	m, _ = send(m, tea.PasteMsg{Content: "boom\x1b]0;pwned\x07"})
	for _, frame := range []string{m.render(), press(m, "enter").render()} {
		rest := sgr.ReplaceAllString(frame, "")
		if strings.ContainsAny(rest, "\x1b\x07\u009b") {
			t.Fatalf("non-SGR control in output: %q", rest)
		}
	}
}

func TestSearchGolden(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		run  func(Model) Model
	}{
		{"search_overlay_140", 140, 20, func(m Model) Model { return press(m, "/", "b", "o") }},
		{"search_active_140", 140, 20, func(m Model) Model { return searchType(press(m, "tab", "g"), "boom") }},
		{"search_active_80", 80, 20, func(m Model) Model { return searchType(press(m, "enter", "g"), "line 3") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.run(searchSetup(t, tc.w, tc.h))
			checkFrame(t, m, tc.w, tc.h)
			got := screen(m)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got != string(want) {
				t.Errorf("%s mismatch:\n%s", path, got)
			}
		})
	}
}
