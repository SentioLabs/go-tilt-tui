package resourcelist

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

var update = flag.Bool("update", false, "rewrite golden files")

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// th returns a theme with no colour, so ansi.Strip output stays stable.
// It builds the theme by hand because theme.ByName isn't implemented on
// this package's base yet.
func th(t *testing.T) theme.Theme {
	t.Helper()
	s := lipgloss.NewStyle()
	return theme.Theme{
		Name: "test",
		Status: map[tilt.Status]lipgloss.Style{
			tilt.StatusNone:      s,
			tilt.StatusPending:   s,
			tilt.StatusBuilding:  s,
			tilt.StatusHealthy:   s,
			tilt.StatusUnhealthy: s,
			tilt.StatusDisabled:  s,
		},
		Border:        s,
		BorderFocused: s,
		Title:         s,
		Dim:           s,
		Selected:      s,
		Match:         s,
		Error:         s,
		Warning:       s,
	}
}

type opt func(*tilt.Resource)

func labels(kv ...string) opt {
	return func(r *tilt.Resource) {
		r.Metadata.Labels = map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			r.Metadata.Labels[kv[i]] = kv[i+1]
		}
	}
}

func status(s tilt.UpdateStatus) opt { return func(r *tilt.Resource) { r.Status.UpdateStatus = s } }
func disabled() opt {
	return func(r *tilt.Resource) { r.Status.DisableStatus = &tilt.DisableStatus{State: "Disabled"} }
}
func built(ago, took time.Duration, warnings ...string) opt {
	return func(r *tilt.Resource) {
		fin := now.Add(-ago)
		start := fin.Add(-took)
		r.Status.BuildHistory = []tilt.BuildRecord{{StartTime: &start, FinishTime: &fin, Warnings: warnings}}
	}
}

func res(name string, opts ...opt) tilt.Resource {
	r := tilt.Resource{Metadata: tilt.ObjectMeta{Name: name}}
	r.Status.UpdateStatus = tilt.UpdateOK
	for _, o := range opts {
		o(&r)
	}
	return r
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func plain(m *Model, w, h int) []string {
	return strings.Split(ansi.Strip(m.View(w, h, true, now)), "\n")
}

func TestGroupKey(t *testing.T) {
	tests := []struct {
		labels map[string]string
		want   string
	}{
		{nil, "ungrouped"},
		{map[string]string{"app": "api", "service": "x"}, "api"},
		{map[string]string{"app": "", "service": "svc"}, "svc"},
		{map[string]string{"app.kubernetes.io/name": "n", "component": "c"}, "n"},
		{map[string]string{"zeta": "z", "alpha": "a"}, "a"},
		{map[string]string{"alpha": "", "beta": "b"}, "b"},
		{map[string]string{"x": ""}, "ungrouped"},
	}
	for _, tt := range tests {
		if got := GroupKey(tt.labels); got != tt.want {
			t.Errorf("GroupKey(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

func TestEveryResourceOnceInItsGroup(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{
		res("(Tiltfile)"),
		res("web", labels("app", "frontend")),
		res("api", labels("app", "backend")),
		res("db", labels("app", "backend")),
		res("misc"),
	})
	got := plain(m, 40, 10)
	want := []string{
		"All Resources",
		"▼ backend (2)",
		"  ●  api",
		"  ●  db",
		"▼ frontend (1)",
		"  ●  web",
		"▼ ungrouped (2)",
		"  ●  (Tiltfile)",
		"  ●  misc",
		"",
	}
	for i := range want {
		if strings.TrimRight(got[i], " ") != want[i] {
			t.Fatalf("row %d = %q, want %q\nall:\n%s", i, got[i], want[i], strings.Join(got, "\n"))
		}
	}
}

func TestNoHeadersWhenAllUngrouped(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("b")})
	got := plain(m, 30, 3)
	if strings.TrimRight(got[1], " ") != "●  a" || strings.TrimRight(got[2], " ") != "●  b" {
		t.Fatalf("rows = %q", got)
	}
}

func TestSelectionFollowsName(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("b"), res("c")})
	m.HandleKey(key("j"))
	m.HandleKey(key("j"))
	if m.Selected() != (logstore.Source{Resource: "b"}) {
		t.Fatalf("Selected = %+v", m.Selected())
	}
	m.SetResources([]tilt.Resource{res("c"), res("b"), res("a")})
	if m.Selected() != (logstore.Source{Resource: "b"}) || m.SelectedResource().Name() != "b" {
		t.Fatalf("selection moved: %+v", m.Selected())
	}
}

func TestHeadersKeepSource(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("api", labels("app", "be")), res("web", labels("app", "fe"))})
	// rows: All, ▼ be, api, ▼ fe, web
	m.HandleKey(key("j")) // header be
	if m.Selected() != (logstore.Source{All: true}) || m.SelectedResource() != nil {
		t.Fatal("header changed the source")
	}
	if !m.OnHeader() {
		t.Fatal("OnHeader false on a group header")
	}
	m.HandleKey(key("j")) // api
	if m.OnHeader() {
		t.Fatal("OnHeader true on a resource row")
	}
	if changed := m.HandleKey(key("j")); changed { // header fe
		t.Fatal("moving onto a header reported a selection change")
	}
	if m.Selected() != (logstore.Source{Resource: "api"}) {
		t.Fatalf("Selected = %+v", m.Selected())
	}
}

func TestCollapseExpand(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("api", labels("app", "be")), res("web", labels("app", "fe"))})
	m.HandleKey(key("j"))
	m.HandleKey(key("j")) // api
	m.HandleKey(key("c"))
	got := plain(m, 30, 4)
	if strings.TrimRight(got[1], " ") != "▶ be (1)" || strings.TrimRight(got[2], " ") != "▶ fe (1)" {
		t.Fatalf("collapsed rows = %q", got)
	}
	m.HandleKey(key("e"))
	m.HandleKey(key("g"))
	m.HandleKey(key("j")) // header be
	m.HandleKey(key("enter"))
	if got := plain(m, 30, 4); strings.TrimRight(got[1], " ") != "▶ be (1)" || strings.TrimRight(got[2], " ") != "▼ fe (1)" {
		t.Fatalf("enter did not toggle one group: %q", got)
	}
}

func TestDisabledToggleAndStickySelection(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("off", disabled()), res("z")})
	m.HandleKey(key("j"))
	m.HandleKey(key("j")) // off
	m.HandleKey(key("d"))
	got := plain(m, 30, 4)
	if !strings.Contains(got[2], "off (filtered)") {
		t.Fatalf("selected disabled row not kept: %q", got)
	}
	if m.Selected() != (logstore.Source{Resource: "off"}) {
		t.Fatal("selection changed when hiding disabled")
	}
	m.HandleKey(key("j")) // z
	for _, row := range plain(m, 30, 4) {
		if strings.Contains(row, "off") {
			t.Fatalf("hidden row still shown after cursor left: %q", row)
		}
	}
}

// sgrParams returns the parameters of the SGR sequence that opens row.
func sgrParams(row string) []string {
	rest, ok := strings.CutPrefix(row, "\x1b[")
	if !ok {
		return nil
	}
	params, _, _ := strings.Cut(rest, "m")
	return strings.Split(params, ";")
}

func TestFocusedCursorKeepsDim(t *testing.T) {
	st := th(t)
	st.Dim = lipgloss.NewStyle().Faint(true)
	st.Selected = lipgloss.NewStyle().Reverse(true)
	m := New(st)
	m.SetResources([]tilt.Resource{res("a"), res("off", disabled()), res("z")})
	m.HandleKey(key("j"))
	m.HandleKey(key("j")) // off
	check := func(name string) {
		t.Helper()
		row := strings.Split(m.View(30, 4, true, now), "\n")[2]
		got := sgrParams(row)
		for _, want := range []string{"2", "7"} {
			if !slices.Contains(got, want) {
				t.Fatalf("%s cursor row %q lacks SGR %s", name, row, want)
			}
		}
	}
	check("disabled")
	m.HandleKey(key("d"))
	check("filtered")
}

func TestCollapseUnpinsFilteredRow(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("api", labels("app", "be")), res("off", labels("app", "be"), disabled())})
	// rows: All, ▼ be, api, off
	m.HandleKey(key("G")) // off
	m.HandleKey(key("d"))
	m.HandleKey(key("c")) // cursor moves to the be header
	m.HandleKey(key("e"))
	for _, row := range plain(m, 30, 5) {
		if strings.Contains(row, "off") {
			t.Fatalf("filtered row kept after cursor left it: %q", row)
		}
	}
}

func TestCounts(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{
		res("ok1"), res("ok2", built(time.Minute, time.Second, "deprecated flag")),
		res("bad", status(tilt.UpdateError)), res("busy", status(tilt.UpdateInProgress)),
		res("wait", status(tilt.UpdatePending)), res("off", disabled()),
	})
	want := Counts{Errors: 1, Warnings: 1, Busy: 2, Healthy: 2, Total: 5}
	if got := m.Counts(); got != want {
		t.Fatalf("Counts = %+v, want %+v", got, want)
	}
}

func TestRowDetails(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{
		res("api", built(11*time.Minute, 11400*time.Millisecond, "w")),
		res("x\x1b]52;c;ZXZpbA==\x07evil"),
	})
	raw := m.View(40, 3, true, now)
	if strings.Contains(raw, "\x1b]") {
		t.Fatal("OSC sequence reached the view")
	}
	rows := plain(m, 40, 3)
	if !strings.HasPrefix(rows[1], "●! api") || !strings.HasSuffix(strings.TrimRight(rows[1], " "), "11m ago · 11.4s") {
		t.Fatalf("api row = %q", rows[1])
	}
	if !strings.Contains(rows[2], "xevil") {
		t.Fatalf("evil row = %q", rows[2])
	}
	if !m.ShowsRelativeTime() {
		t.Fatal("ShowsRelativeTime false with a time on screen")
	}
	m.View(24, 3, true, now)
	if m.ShowsRelativeTime() {
		t.Fatal("time drawn below 30 columns")
	}
}

func TestViewSizeAndScroll(t *testing.T) {
	m := New(th(t))
	var rs []tilt.Resource
	for i := range 30 {
		rs = append(rs, res(fmt.Sprintf("res-%02d-with-a-long-name", i)))
	}
	m.SetResources(rs)
	m.HandleKey(key("G"))
	out := m.View(24, 8, true, now)
	lines := strings.Split(out, "\n")
	if len(lines) != 8 {
		t.Fatalf("View returned %d lines, want 8", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 24 {
			t.Fatalf("line %d is %d wide", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(lines[7]), "res-29") {
		t.Fatalf("cursor row not visible: %q", ansi.Strip(out))
	}
}

func TestGolden(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{
		res("(Tiltfile)", built(13*time.Minute, 2*time.Second)),
		res("api", labels("app", "backend"), built(11*time.Minute, 11400*time.Millisecond)),
		res("worker", labels("app", "backend"), status(tilt.UpdateError), built(30*time.Second, 3*time.Second)),
		res("web", labels("app", "frontend"), status(tilt.UpdateInProgress)),
		res("old", labels("app", "frontend"), disabled()),
	})
	for _, w := range []int{24, 48} {
		name := filepath.Join("testdata", fmt.Sprintf("list_%d.golden", w))
		got := ansi.Strip(m.View(w, 10, true, now))
		if *update {
			if err := os.WriteFile(name, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%v (run with -update)", err)
		}
		if got != string(want) {
			t.Errorf("%s mismatch:\n%s", name, got)
		}
	}
}

// sgrBefore returns the parameters of the SGR sequence that directly
// precedes the first glyph in row, or nil when glyph isn't styled.
func sgrBefore(row, glyph string) []string {
	head, _, ok := strings.Cut(row, glyph)
	if !ok || !strings.HasSuffix(head, "m") {
		return nil
	}
	return sgrParams(head[strings.LastIndex(head, "\x1b["):])
}

func TestDotAndWarningStyles(t *testing.T) {
	st := th(t)
	for s := range st.Status {
		st.Status[s] = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(1 + int(s)))
	}
	st.Warning = lipgloss.NewStyle().Foreground(lipgloss.BrightYellow)
	want := func(s lipgloss.Style) []string {
		t.Helper()
		p := sgrParams(s.Render("x"))
		if len(p) == 0 {
			t.Fatal("test style renders no SGR")
		}
		return p
	}

	rs := []tilt.Resource{
		res("bad", status(tilt.UpdateError)),
		res("good", built(time.Minute, time.Second, "w")),
	}
	m := New(st)
	m.SetResources(rs)
	// rows: All (cursor), bad, good
	rows := strings.Split(m.View(40, 3, true, now), "\n")
	var dots [][]string
	for i, r := range rs {
		row := rows[i+1]
		got := sgrBefore(row, "●")
		if exp := want(st.Status[r.Combined()]); !slices.Equal(got, exp) {
			t.Fatalf("%s dot SGR = %v, want %v (Status[%v]) in %q", r.Metadata.Name, got, exp, r.Combined(), row)
		}
		dots = append(dots, got)
	}
	if slices.Equal(dots[0], dots[1]) {
		t.Fatalf("both dots share SGR %v", dots[0])
	}
	if got, exp := sgrBefore(rows[2], "!"), want(st.Warning); !slices.Equal(got, exp) {
		t.Fatalf("warning SGR = %v, want %v in %q", got, exp, rows[2])
	}
}

func waitingOn(names ...string) opt {
	return func(r *tilt.Resource) {
		w := &tilt.Waiting{Reason: "waiting-for-local"}
		for _, n := range names {
			w.On = append(w.On, struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			}{Kind: "UIResource", Name: n})
		}
		r.Status.Waiting = w
	}
}

func newList(t *testing.T, rs ...tilt.Resource) *Model {
	m := New(th(t))
	m.SetResources(rs)
	return m
}

func TestWaitingOnReplacesTime(t *testing.T) {
	// "web" has a finished build, so without waiting it would show "… ago".
	m := newList(t, res("web", built(11*time.Minute, 2*time.Second), waitingOn("api", "db")))
	out := ansi.Strip(m.View(48, 5, true, now))
	if !strings.Contains(out, "waiting on api, db") || strings.Contains(out, "ago") {
		t.Fatalf("row:\n%s", out)
	}
	if m.ShowsRelativeTime() {
		t.Fatal("waiting text must not start the relative-time tick")
	}
}

func TestWaitingOnCutToFit(t *testing.T) {
	m := newList(t, res("frontend-server", waitingOn("database-primary", "cache", "queue")))
	for _, line := range strings.Split(ansi.Strip(m.View(32, 5, true, now)), "\n") {
		if ansi.StringWidth(line) > 32 {
			t.Fatalf("line wider than 32: %q", line)
		}
	}
	out := ansi.Strip(m.View(32, 5, true, now))
	if !strings.Contains(out, "frontend") || !strings.Contains(out, "waiting on") || !strings.Contains(out, "…") {
		t.Fatalf("want the name start, a cut waiting text and an ellipsis:\n%s", out)
	}
}

func TestWaitingOnHiddenWhenNarrow(t *testing.T) {
	m := newList(t, res("web", waitingOn("api")))
	if out := ansi.Strip(m.View(24, 5, true, now)); strings.Contains(out, "waiting") {
		t.Fatalf("narrow pane showed waiting text:\n%s", out)
	}
}

func TestWaitingOnNamesAreSafe(t *testing.T) {
	m := newList(t, res("web", waitingOn("api\x1b]0;pwned\x07")))
	if out := m.View(48, 5, true, now); strings.Contains(out, "\x1b]") {
		t.Fatalf("OSC reached the row: %q", out)
	}
}

func TestNoWaitingKeepsTime(t *testing.T) {
	m := newList(t, res("web", built(11*time.Minute, 2*time.Second), func(r *tilt.Resource) { r.Status.Waiting = &tilt.Waiting{Reason: "x"} }))
	if out := ansi.Strip(m.View(48, 5, true, now)); !strings.Contains(out, "ago") {
		t.Fatalf("empty On hid the time:\n%s", out)
	}
}

func TestWaitingOnIsDim(t *testing.T) {
	st := th(t)
	st.Dim = lipgloss.NewStyle().Faint(true)
	m := New(st)
	m.SetResources([]tilt.Resource{res("web", waitingOn("api"))})
	// rows: All (cursor), web
	row := strings.Split(m.View(48, 2, true, now), "\n")[1]
	if got := sgrBefore(row, "waiting on"); !slices.Contains(got, "2") {
		t.Fatalf("waiting text SGR = %v, want faint in %q", got, row)
	}
}

func TestClickRow(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("api", labels("app", "be")), res("web", labels("app", "fe"))})
	// rows: All, ▼ be, api, ▼ fe, web
	plain(m, 30, 5)
	if !m.ClickRow(2) || m.Selected() != (logstore.Source{Resource: "api"}) {
		t.Fatalf("click on api: Selected = %+v", m.Selected())
	}
	// An unselected header only takes the cursor and keeps the source.
	if !m.ClickRow(1) || !m.OnHeader() || m.Selected() != (logstore.Source{Resource: "api"}) {
		t.Fatalf("click on header be: OnHeader %v, Selected %+v", m.OnHeader(), m.Selected())
	}
	if got := plain(m, 30, 5); strings.TrimRight(got[1], " ") != "▼ be (1)" {
		t.Fatalf("first click collapsed the header: %q", got)
	}
	// A click on the selected header toggles it, like enter.
	if !m.ClickRow(1) {
		t.Fatal("second click on header reported no row")
	}
	if got := plain(m, 30, 5); strings.TrimRight(got[1], " ") != "▶ be (1)" {
		t.Fatalf("second click did not collapse: %q", got)
	}
	m.ClickRow(1)
	if got := plain(m, 30, 5); strings.TrimRight(got[1], " ") != "▼ be (1)" {
		t.Fatalf("third click did not expand: %q", got)
	}
}

func TestClickRowScrolled(t *testing.T) {
	m := New(th(t))
	var rs []tilt.Resource
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		rs = append(rs, res(n))
	}
	m.SetResources(rs) // rows: All, a .. j
	m.HandleKey(key("G"))
	got := plain(m, 30, 3) // draws h, i, j
	if !m.ClickRow(0) {
		t.Fatal("click on the first drawn line reported no row")
	}
	if m.Selected() != (logstore.Source{Resource: "h"}) {
		t.Fatalf("click hit the row from the top, not the drawn row: %+v (screen %q)", m.Selected(), got)
	}
}

func TestClickRowMisses(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("b"), res("c")})
	if m.ClickRow(0) {
		t.Fatal("ClickRow hit a row before any View")
	}
	plain(m, 30, 6) // rows All, a, b, c on lines 0..3; lines 4, 5 are empty
	m.HandleKey(key("j"))
	for _, i := range []int{-1, 4, 5, 6, 100} {
		if m.ClickRow(i) {
			t.Errorf("ClickRow(%d) reported a row", i)
		}
		if m.Selected() != (logstore.Source{Resource: "a"}) {
			t.Fatalf("ClickRow(%d) moved the cursor: %+v", i, m.Selected())
		}
	}
}
