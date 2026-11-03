package overlay

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ Overlay                                    = (*Watches)(nil)
	_ func(theme.Theme, string) *Watches         = NewWatches
	_ func(*Watches, []tilt.FileWatch, error)    = (*Watches).SetWatches
	_ func(*Watches, int, int) string            = (*Watches).View
	_ func(*Watches, tea.Msg) (Overlay, tea.Cmd) = (*Watches).Update
	_ error                                      = ErrNeedsLocalTilt
	_ func(*Watches) string                      = (*Watches).Title
)

// --- Behavior tests ---

// watchesAt is 23:05:<sec> local time on a fixed day.
func watchesAt(sec int) time.Time { return time.Date(2026, 11, 3, 23, 5, sec, 0, time.Local) }

// watchesFixture is one api watch with two paths, one ignore, three events
// (one with a duplicated seen file) and an error, then one for (Tiltfile).
func watchesFixture() []tilt.FileWatch {
	return []tilt.FileWatch{
		{
			Name:         "local:api",
			Resource:     "api",
			WatchedPaths: []string{"/work/src", "/work/go.mod"},
			Ignores:      []tilt.IgnoreDef{{BasePath: "/work", Patterns: []string{"src/*.tmp"}}},
			Events: []tilt.FileEvent{
				{Time: watchesAt(40), SeenFiles: []string{"/work/src/a.txt", "/work/src/c.txt", "/work/src/a.txt"}},
				{Time: watchesAt(44), SeenFiles: []string{"/work/src/b.txt"}},
				{Time: watchesAt(30), SeenFiles: []string{"/work/go.mod"}},
			},
			Error: "too many files",
		},
		{
			Name:         "configs:(Tiltfile)",
			Resource:     "(Tiltfile)",
			WatchedPaths: []string{"/work/Tiltfile"},
		},
	}
}

func watchesPanel(t *testing.T, resource string) *Watches {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return NewWatches(th, resource)
}

func watchesText(w *Watches) string { return ansi.Strip(w.View(120, 100)) }

func TestWatchesResourceView(t *testing.T) {
	w := watchesPanel(t, "api")
	w.SetWatches(watchesFixture(), nil)
	out := watchesText(w)
	for _, want := range []string{
		"local:api",
		"  watching  /work/src",
		"/work/go.mod",
		"  ignoring  /work: src/*.tmp",
		"  events    23:05:44  /work/src/b.txt",
		"            23:05:40  /work/src/a.txt, /work/src/c.txt\n",
		"            23:05:30  /work/go.mod",
		"  error     too many files",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("api view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Tiltfile") {
		t.Errorf("api view shows another resource's watch:\n%s", out)
	}
	if strings.Contains(out, "api · ") {
		t.Errorf("resource view prefixes the name:\n%s", out)
	}
	if i, j := strings.Index(out, "23:05:44"), strings.Index(out, "23:05:30"); i > j {
		t.Errorf("events are not newest first:\n%s", out)
	}
	if got := w.Title(); got != "File watches · api" {
		t.Errorf("Title = %q", got)
	}
}

func TestWatchesAllView(t *testing.T) {
	w := watchesPanel(t, "")
	w.SetWatches(watchesFixture(), nil)
	out := watchesText(w)
	for _, want := range []string{"api · local:api", "(Tiltfile) · configs:(Tiltfile)", "  watching  /work/Tiltfile", "\n\n(Tiltfile) · "} {
		if !strings.Contains(out, want) {
			t.Errorf("all view lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "  events    none") {
		t.Errorf("a watch without events does not say none:\n%s", out)
	}
	if got := w.Title(); got != "File watches · all" {
		t.Errorf("Title = %q", got)
	}
}

func TestWatchesShowsNewestTenEvents(t *testing.T) {
	var events []tilt.FileEvent
	for i := range 12 {
		events = append(events, tilt.FileEvent{Time: watchesAt(i), SeenFiles: []string{fmt.Sprintf("/f%02d", i)}})
	}
	w := watchesPanel(t, "api")
	w.SetWatches([]tilt.FileWatch{{Name: "local:api", Resource: "api", Events: events}}, nil)
	out := watchesText(w)
	for i := range 12 {
		f := fmt.Sprintf("/f%02d", i)
		if shown := strings.Contains(out, f); shown != (i >= 2) {
			t.Errorf("event %s shown = %v:\n%s", f, shown, out)
		}
	}
	if !strings.Contains(out, "  events    23:05:11  /f11") {
		t.Errorf("newest event is not first:\n%s", out)
	}
}

func TestWatchesLoadingAndErrors(t *testing.T) {
	w := watchesPanel(t, "api")
	if out := watchesText(w); out != "loading file watches…" {
		t.Errorf("before SetWatches = %q", out)
	}
	w.SetWatches(nil, ErrNeedsLocalTilt)
	if out := watchesText(w); out != "file-watch info needs a local Tilt" {
		t.Errorf("remote = %q", out)
	}
	w.SetWatches(nil, errors.New("boom\x1b]0;x\x07"))
	raw := w.View(120, 100)
	if strings.Contains(raw, "\x1b]") {
		t.Errorf("error OSC reached View: %q", raw)
	}
	if out := ansi.Strip(raw); !strings.HasPrefix(out, "file watches failed: boom") {
		t.Errorf("failure = %q", out)
	}
}

func TestWatchesEmpty(t *testing.T) {
	w := watchesPanel(t, "web")
	w.SetWatches(watchesFixture(), nil)
	if out := watchesText(w); out != "no file watches for web" {
		t.Errorf("no match = %q", out)
	}
	all := watchesPanel(t, "")
	all.SetWatches(nil, nil)
	if out := watchesText(all); out != "no file watches" {
		t.Errorf("empty all = %q", out)
	}
}

func TestWatchesTermsafe(t *testing.T) {
	osc := "\x1b]52;c;ZXZpbA==\x07"
	w := watchesPanel(t, "")
	w.SetWatches([]tilt.FileWatch{{
		Name:         "local:x" + osc,
		Resource:     "x" + osc,
		WatchedPaths: []string{"/work" + osc},
		Ignores:      []tilt.IgnoreDef{{BasePath: "/b" + osc, Patterns: []string{"*" + osc}}},
		Events:       []tilt.FileEvent{{Time: watchesAt(1), SeenFiles: []string{"/e" + osc}}},
		Error:        "bad" + osc,
	}}, nil)
	raw := w.View(200, 100)
	if strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") {
		t.Errorf("OSC reached View: %q", raw)
	}
	if !strings.Contains(ansi.Strip(raw), "  watching  /work") {
		t.Errorf("path missing:\n%s", ansi.Strip(raw))
	}
}

func TestWatchesScrollAndClose(t *testing.T) {
	w := watchesPanel(t, "")
	w.SetWatches(watchesFixture(), nil)
	first := w.View(40, 6)
	lines := strings.Split(first, "\n")
	if len(lines) > 6 {
		t.Fatalf("View(40, 6) has %d lines", len(lines))
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > 40 {
			t.Errorf("line %d is %d wide: %q", i, lw, l)
		}
	}
	if !strings.HasPrefix(ansi.Strip(first), "api · local:api") {
		t.Fatalf("panel does not start at the top:\n%s", ansi.Strip(first))
	}
	down := tea.KeyPressMsg{Code: 'j', Text: "j"}
	var ov Overlay = w
	for range 30 {
		ov, _ = ov.Update(down)
	}
	last := ansi.Strip(ov.View(40, 6))
	if !strings.Contains(last, "/work/Tiltfile") || strings.Contains(last, "api · local:api") {
		t.Fatalf("j did not scroll to the end:\n%s", last)
	}
	ov, _ = ov.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if back := ansi.Strip(ov.View(40, 6)); back == last {
		t.Fatalf("k did not scroll up:\n%s", back)
	}
	ov, _ = ov.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	ov, _ = ov.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	ov, _ = ov.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if up := ansi.Strip(ov.View(40, 6)); up == last {
		t.Fatalf("up did not scroll:\n%s", up)
	}
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'w', Text: "w"}} {
		_, cmd := ov.Update(k)
		if cmd == nil {
			t.Fatalf("%s did not close the panel", k.String())
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%s did not send CloseMsg", k.String())
		}
	}
}
