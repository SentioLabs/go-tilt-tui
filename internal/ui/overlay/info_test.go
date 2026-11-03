package overlay

import (
	"errors"
	"fmt"
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
	_ Overlay                                 = (*Info)(nil)
	_ func(theme.Theme, tilt.Resource) *Info  = NewInfo
	_ func(*Info) string                      = (*Info).Resource
	_ func(*Info, map[string][]string, error) = (*Info).SetDeps
	_ error                                   = ErrDepsUnavailable
)

// --- Behavior tests ---

// infoGraph is the dependency map most tests load: web needs api and db,
// and api needs db.
var infoGraph = map[string][]string{"web": {"api", "db"}, "api": {"db"}, "db": nil}

// infoResource returns a local resource named name with a failed build,
// a wait, one named and one bare link, and a pod.
func infoResource(name string) tilt.Resource {
	var r tilt.Resource
	r.Metadata.Name = name
	r.Status.Order = 2
	r.Status.Specs = []tilt.TargetSpec{{ID: "local:" + name, Type: "local"}}
	r.Status.UpdateStatus = tilt.UpdateError
	r.Status.BuildHistory = []tilt.BuildRecord{{Error: "boom\nsecond line"}}
	r.Status.Waiting = &tilt.Waiting{Reason: "waiting-for-local"}
	r.Status.Waiting.On = append(r.Status.Waiting.On,
		struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}{Kind: "UIResource", Name: "never"},
		struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}{Kind: "UIResource", Name: "db"},
	)
	r.Status.EndpointLinks = []tilt.EndpointLink{{URL: "http://localhost:8080"}, {URL: "http://localhost:9090", Name: "metrics"}}
	r.Status.K8sResourceInfo = &tilt.K8sResourceInfo{PodName: "api-7d9", PodStatus: "Running", PodRestarts: 2}
	return r
}

func infoTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return th
}

// infoLines returns View's lines with SGR stripped and trailing spaces cut.
func infoLines(i *Info) []string {
	got := strings.Split(ansi.Strip(i.View(120, 100)), "\n")
	for n := range got {
		got[n] = strings.TrimRight(got[n], " ")
	}
	return got
}

// infoAfter returns the lines that follow the line equal to head.
func infoAfter(t *testing.T, lines []string, head string) []string {
	t.Helper()
	n := slices.Index(lines, head)
	if n < 0 {
		t.Fatalf("no %q line in %q", head, lines)
	}
	return lines[n+1:]
}

func infoKey(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func TestInfoDetails(t *testing.T) {
	i := NewInfo(infoTheme(t), infoResource("api"))
	got := infoLines(i)
	want := []string{
		"kind        local",
		"status      ● error",
		"waiting on  never, db  (waiting-for-local)",
		"link        http://localhost:8080",
		"link        metrics  http://localhost:9090",
		"pod         api-7d9 · Running · 2 restarts",
		"last error  boom",
		"",
		"depends on  loading…",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("View =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if i.Title() != "Info · api" || i.Resource() != "api" {
		t.Fatalf("Title = %q, Resource = %q", i.Title(), i.Resource())
	}
}

func TestInfoOptionalRows(t *testing.T) {
	var r tilt.Resource
	r.Metadata.Name = "plain"
	r.Status.Specs = []tilt.TargetSpec{{Type: "k8s"}}
	r.Status.BuildHistory = []tilt.BuildRecord{{Warnings: []string{"w1", "w2"}}}
	r.Status.DisableStatus = &tilt.DisableStatus{State: "Disabled"}
	got := infoLines(NewInfo(infoTheme(t), r))
	for _, want := range []string{"warnings    2", "disabled    yes"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %q row in %q", want, got)
		}
	}
	for _, l := range got {
		for _, absent := range []string{"waiting on", "link", "pod", "last error"} {
			if strings.HasPrefix(l, absent) {
				t.Errorf("row %q shown without data", l)
			}
		}
	}
}

func TestInfoDependencyTree(t *testing.T) {
	i := NewInfo(infoTheme(t), infoResource("web"))
	i.SetDeps(infoGraph, nil)
	got := infoAfter(t, infoLines(i), "depends on")
	want := []string{"  ├─ api", "  │  └─ db", "  └─ db", "used by  none"}
	if !slices.Equal(got, want) {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestInfoUsedBy(t *testing.T) {
	i := NewInfo(infoTheme(t), infoResource("db"))
	i.SetDeps(infoGraph, nil)
	lines := infoLines(i)
	if !slices.Contains(lines, "depends on  none") {
		t.Fatalf("no %q line in %q", "depends on  none", lines)
	}
	if got, want := infoAfter(t, lines, "used by"), []string{"  api", "  web"}; !slices.Equal(got, want) {
		t.Fatalf("used by = %q, want %q", got, want)
	}
}

func TestInfoCycle(t *testing.T) {
	i := NewInfo(infoTheme(t), infoResource("a"))
	i.SetDeps(map[string][]string{"a": {"b"}, "b": {"a"}}, nil)
	got := infoAfter(t, infoLines(i), "depends on")
	want := []string{"  └─ b", "     └─ a (cycle)", "used by"}
	if !slices.Equal(got[:3], want) {
		t.Fatalf("tree = %q, want %q", got, want)
	}
}

func TestInfoDepthStops(t *testing.T) {
	deps := map[string][]string{}
	for n := range 20 {
		deps[fmt.Sprint("r", n)] = []string{fmt.Sprint("r", n+1)}
	}
	i := NewInfo(infoTheme(t), infoResource("r0"))
	i.SetDeps(deps, nil)
	tree := infoAfter(t, infoLines(i), "depends on")
	n := slices.IndexFunc(tree, func(l string) bool { return !strings.HasPrefix(l, "  ") })
	if n != 8 {
		t.Fatalf("tree has %d rows, want 8: %q", n, tree)
	}
}

func TestInfoErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("decode: %w", tilt.ErrUnknownFormat), "dependency info unavailable for this Tilt version"},
		{fmt.Errorf("engine dump: %w", tilt.ErrTooLarge), "engine dump too large (over 64 MiB)"},
		{ErrDepsUnavailable, "dependency info unavailable"},
		{errors.New("connection refused"), "dependency info failed: connection refused"},
	} {
		i := NewInfo(infoTheme(t), infoResource("api"))
		i.SetDeps(map[string][]string{"api": {"db"}}, tc.err)
		lines := infoLines(i)
		if last := lines[len(lines)-1]; last != tc.want {
			t.Errorf("%v: last line %q, want %q", tc.err, last, tc.want)
		}
		if slices.Contains(lines, "  └─ db") {
			t.Errorf("%v: tree drawn despite the error", tc.err)
		}
	}
}

func TestInfoTextPassesThroughTermsafe(t *testing.T) {
	evil := "d\x1b]52;c;ZXZpbA==\x07b"
	r := infoResource("a\x1b]0;t\x07pi")
	r.Status.BuildHistory[0].Error = "bad\x1b]0;pwned\x07"
	r.Status.EndpointLinks = []tilt.EndpointLink{{URL: "http://x\x1b]8;;evil\x07", Name: "n\x1b]0;t\x07"}}
	i := NewInfo(infoTheme(t), r)
	i.SetDeps(map[string][]string{"a\x1b]0;t\x07pi": {evil}, evil: nil, "w\x1b]0;t\x07eb": {"a\x1b]0;t\x07pi"}}, nil)
	if out := i.View(120, 100); strings.Contains(out, "\x1b]") || strings.Contains(out, "\x07") {
		t.Fatalf("OSC in view: %q", out)
	}
	if lines := infoLines(i); !slices.Contains(lines, "  └─ db") || !slices.Contains(lines, "  web") {
		t.Fatalf("names lost: %q", lines)
	}

	i = NewInfo(infoTheme(t), infoResource("api"))
	i.SetDeps(nil, errors.New("dial\x1b]52;c;ZXZpbA==\x07 failed"))
	if out := i.View(120, 100); strings.Contains(out, "\x1b]") {
		t.Fatalf("OSC in error view: %q", out)
	}
}

func TestInfoViewFitsAndScrolls(t *testing.T) {
	i := NewInfo(infoTheme(t), infoResource("web"))
	i.SetDeps(infoGraph, nil)
	view := func() []string { return strings.Split(i.View(40, 5), "\n") }
	first := view()
	if len(first) > 5 {
		t.Fatalf("View(40, 5) has %d lines", len(first))
	}
	for _, l := range first {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("line %q is %d wide", l, w)
		}
	}
	all := strings.Split(i.View(200, 100), "\n")
	i.View(40, 5)

	i.Update(infoKey("j"))
	if got := view(); got[0] != ansi.Truncate(all[1], 40, "…") {
		t.Fatalf("after j, first line %q, want %q", got[0], all[1])
	}
	i.Update(infoKey("down"))
	i.Update(infoKey("up"))
	i.Update(infoKey("k"))
	if got := view(); got[0] != first[0] {
		t.Fatalf("after k, first line %q, want %q", got[0], first[0])
	}
	i.Update(infoKey("k"))
	if got := view(); got[0] != first[0] {
		t.Fatal("k scrolled above the top")
	}
	for range 50 {
		i.Update(infoKey("j"))
	}
	if got := view(); got[len(got)-1] != ansi.Truncate(all[len(all)-1], 40, "…") {
		t.Fatalf("j scrolled past the end: %q", got)
	}
}

func TestInfoCloses(t *testing.T) {
	for _, k := range []string{"esc", "i"} {
		i := NewInfo(infoTheme(t), infoResource("api"))
		_, cmd := i.Update(infoKey(k))
		if cmd == nil {
			t.Fatalf("%s returned no command", k)
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%s did not close", k)
		}
	}
	i := NewInfo(infoTheme(t), infoResource("api"))
	if _, cmd := i.Update(infoKey("x")); cmd != nil {
		t.Fatal("x returned a command")
	}
}
