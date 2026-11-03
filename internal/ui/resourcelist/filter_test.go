package resourcelist

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ func(Filter) string              = Filter.String
	_ func(Filter) Filter              = Filter.Next
	_ func(Filter, tilt.Resource) bool = Filter.Match
	_ func(*Model) Filter              = (*Model).Filter
	_ func(*Model, Filter)             = (*Model).SetFilter
)

// --- Behavior tests ---

// filterSet has one resource in each status, in two groups.
func filterSet() []tilt.Resource {
	return []tilt.Resource{
		res("ok", labels("app", "be")),
		res("bad", labels("app", "be"), status(tilt.UpdateError)),
		res("busy", labels("app", "fe"), status(tilt.UpdateInProgress)),
		res("wait", labels("app", "fe"), status(tilt.UpdatePending)),
		res("off", labels("app", "fe"), disabled()),
	}
}

// names lists the resource names on screen, in order.
func names(m *Model) []string {
	var out []string
	for _, l := range plain(m, 40, 12) {
		l = strings.TrimSpace(l)
		if name, ok := strings.CutPrefix(l, "●  "); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestFilterCycle(t *testing.T) {
	want := []Filter{FilterErrors, FilterInProgress, FilterHealthy, FilterAll}
	f := FilterAll
	for i, w := range want {
		f = f.Next()
		if f != w {
			t.Fatalf("step %d: Next = %v, want %v", i, f, w)
		}
	}
	got := []string{FilterAll.String(), FilterErrors.String(), FilterInProgress.String(), FilterHealthy.String()}
	if !slices.Equal(got, []string{"all", "errors", "in progress", "healthy"}) {
		t.Fatalf("names = %q", got)
	}
}

func TestFilterMatchesCombined(t *testing.T) {
	m := New(th(t))
	m.SetResources(filterSet())
	tests := []struct {
		f    Filter
		want []string
	}{
		{FilterAll, []string{"ok", "bad", "busy", "wait", "off"}},
		{FilterErrors, []string{"bad"}},
		{FilterInProgress, []string{"busy", "wait"}},
		{FilterHealthy, []string{"ok"}},
	}
	for _, tt := range tests {
		m.SetFilter(tt.f)
		if got := names(m); !slices.Equal(got, tt.want) {
			t.Errorf("filter %v lists %q, want %q", tt.f, got, tt.want)
		}
		if m.Filter() != tt.f {
			t.Errorf("Filter = %v, want %v", m.Filter(), tt.f)
		}
	}
}

func TestFilterHidesEmptiedGroups(t *testing.T) {
	m := New(th(t))
	m.SetResources(filterSet())
	m.SetFilter(FilterErrors)
	got := strings.Join(plain(m, 40, 6), "\n")
	if !strings.Contains(got, "▼ be (2)") || strings.Contains(got, "fe") {
		t.Fatalf("errors filter rows:\n%s", got)
	}
}

func TestFilterKeepsSelectedResource(t *testing.T) {
	// th has no styles at all. The mono theme dims with SGR 2 (faint).
	mono, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	m := New(mono)
	m.SetResources(filterSet())
	if !m.Select("busy") {
		t.Fatal("Select(busy) = false")
	}
	m.SetFilter(FilterErrors)
	if m.Selected() != (logstore.Source{Resource: "busy"}) {
		t.Fatalf("filter moved the selection to %+v", m.Selected())
	}
	rows := plain(m, 40, 8)
	if !slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "busy (filtered)") }) {
		t.Fatalf("selected resource not kept:\n%s", strings.Join(rows, "\n"))
	}
	raw := m.View(40, 8, true, now)
	var busyRow string
	for _, l := range strings.Split(raw, "\n") {
		if strings.Contains(ansi.Strip(l), "busy") {
			busyRow = l
		}
	}
	if !regexp.MustCompile(`\x1b\[([0-9;]*;)?2[;m]`).MatchString(busyRow) {
		t.Fatalf("kept row is not dimmed: %q", busyRow)
	}
	m.HandleKey(key("g"))
	if got := names(m); !slices.Equal(got, []string{"bad"}) {
		t.Fatalf("after the cursor left, rows = %q", got)
	}
}

func TestFilterKeepsSelectionWhenStatusChanges(t *testing.T) {
	m := New(th(t))
	rs := filterSet()
	m.SetResources(rs)
	m.SetFilter(FilterErrors)
	m.Select("bad")
	rs[1] = res("bad", labels("app", "be")) // the build now passes
	m.SetResources(rs)
	if m.Selected() != (logstore.Source{Resource: "bad"}) {
		t.Fatalf("a status change moved the selection to %+v", m.Selected())
	}
	if got := strings.Join(plain(m, 40, 6), "\n"); !strings.Contains(got, "bad (filtered)") {
		t.Fatalf("selected resource not kept:\n%s", got)
	}
}

func TestCountsIgnoreFilter(t *testing.T) {
	m := New(th(t))
	m.SetResources(filterSet())
	want := m.Counts()
	for _, f := range []Filter{FilterErrors, FilterInProgress, FilterHealthy} {
		m.SetFilter(f)
		if got := m.Counts(); got != want {
			t.Fatalf("filter %v: Counts = %+v, want %+v", f, got, want)
		}
	}
}

func TestFilterGolden(t *testing.T) {
	m := New(th(t))
	m.SetResources(filterSet())
	m.Select("ok")
	m.SetFilter(FilterInProgress)
	got := ansi.Strip(m.View(48, 8, true, now))
	name := filepath.Join("testdata", "list_filter_48.golden")
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
