package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// --- Contract assertions ---

var (
	_ time.Duration        = watchesTimeout
	_ func(string) tea.Cmd = (&Model{}).openWatches
	_                      = watchesMsg{target: (*overlay.Watches)(nil), ws: []tilt.FileWatch(nil), err: error(nil)}
)

// --- Behavior tests ---

// watchFake records Deps.FileWatches calls and returns ws.
type watchFake struct {
	ws          []tilt.FileWatch
	calls       int
	hadDeadline bool
}

func (f *watchFake) fileWatches(ctx context.Context) ([]tilt.FileWatch, error) {
	f.calls++
	_, f.hadDeadline = ctx.Deadline()
	return f.ws, nil
}

// watchAt is 23:05:<sec> local time on a fixed day.
func watchAt(sec int) time.Time { return time.Date(2026, 11, 3, 23, 5, sec, 0, time.Local) }

func watchWatches() []tilt.FileWatch {
	return []tilt.FileWatch{
		{
			Name:         "configs:(Tiltfile)",
			Resource:     "(Tiltfile)",
			WatchedPaths: []string{"/work/Tiltfile"},
		},
		{
			Name:         "local:api",
			Resource:     "api",
			WatchedPaths: []string{"/work/src"},
			Ignores:      []tilt.IgnoreDef{{BasePath: "/work", Patterns: []string{"src/*.tmp"}}},
			Events: []tilt.FileEvent{
				{Time: watchAt(40), SeenFiles: []string{"/work/src/a.txt", "/work/src/a.txt"}},
				{Time: watchAt(44), SeenFiles: []string{"/work/src/b.txt"}},
			},
		},
	}
}

// watchSetup is setup with only the watch feature and fake as
// Deps.FileWatches.
func watchSetup(t *testing.T, fake *watchFake) Model {
	t.Helper()
	withFeatures(t, watchFeature)
	d := Deps{}
	if fake != nil {
		d.FileWatches = fake.fileWatches
	}
	m, _ := setup(t, 140, 30, d)
	return m
}

func watchTop(t *testing.T, m Model) *overlay.Watches {
	t.Helper()
	if len(m.overlays) != 1 {
		t.Fatalf("%d overlays open", len(m.overlays))
	}
	w, ok := m.overlays[0].(*overlay.Watches)
	if !ok {
		t.Fatalf("top overlay is %T", m.overlays[0])
	}
	return w
}

func TestWatchOpensAllOnAllRow(t *testing.T) {
	fake := &watchFake{ws: watchWatches()}
	m := watchSetup(t, fake)
	m = press(m, "w")
	if got := watchTop(t, m).Title(); got != "File watches · all" {
		t.Fatalf("Title = %q", got)
	}
	if fake.calls != 1 {
		t.Fatalf("FileWatches called %d times", fake.calls)
	}
	s := screen(m)
	for _, want := range []string{"api · local:api", "(Tiltfile) · configs:(Tiltfile)"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	m = press(m, "w")
	if len(m.overlays) != 0 {
		t.Fatal("w did not close the panel")
	}
}

func TestWatchOpensSelectedResource(t *testing.T) {
	fake := &watchFake{ws: watchWatches()}
	m := watchSetup(t, fake)
	m = press(m, "j", "j")
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("cursor not on api: %v", r)
	}
	nm, cmd := m.Update(key("w"))
	m = nm.(Model)
	msgs := drain(cmd)
	for _, msg := range msgs {
		if _, ok := msg.(PushOverlayMsg); ok {
			nm, _ = m.Update(msg)
			m = nm.(Model)
		}
	}
	if got := watchTop(t, m).Title(); got != "File watches · api" {
		t.Fatalf("Title = %q", got)
	}
	if s := screen(m); !strings.Contains(s, "loading file watches…") {
		t.Fatalf("panel is not loading before the result:\n%s", s)
	}
	for _, msg := range msgs {
		if _, ok := msg.(watchesMsg); ok {
			m, _ = send(m, msg)
		}
	}
	s := screen(m)
	for _, want := range []string{"local:api", "watching  /work/src", "ignoring  /work: src/*.tmp", "events    23:05:44  /work/src/b.txt"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "/work/Tiltfile") {
		t.Errorf("api panel shows another resource's watch:\n%s", s)
	}
	if !fake.hadDeadline {
		t.Error("FileWatches got a context without a deadline")
	}
}

func TestWatchNeedsLocalTilt(t *testing.T) {
	m := watchSetup(t, nil)
	nm, cmd := m.Update(key("w"))
	m = nm.(Model)
	msgs := drain(cmd)
	if len(msgs) != 1 {
		t.Fatalf("w without FileWatches produced %d messages: %v", len(msgs), msgs)
	}
	if _, ok := msgs[0].(PushOverlayMsg); !ok {
		t.Fatalf("w produced %T", msgs[0])
	}
	m, _ = send(m, msgs[0])
	if s := screen(m); !strings.Contains(s, "file-watch info needs a local Tilt") {
		t.Fatalf("panel does not say it needs a local Tilt:\n%s", s)
	}
}

func TestWatchesGolden(t *testing.T) {
	fake := &watchFake{ws: watchWatches()}
	withFeatures(t, watchFeature)
	m, _ := setup(t, 140, 30, Deps{FileWatches: fake.fileWatches})
	m = press(m, "j", "j", "w")
	checkFrame(t, m, 140, 30)
	got := screen(m)
	path := filepath.Join("testdata", "watches_140.golden")
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
}

func TestWatchesOnlySGRReachesTheTerminal(t *testing.T) {
	osc := "\x1b]52;c;ZXZpbA==\x07"
	fake := &watchFake{ws: []tilt.FileWatch{{
		Name:         "local:api",
		Resource:     "api",
		WatchedPaths: []string{"/work" + osc},
		Events:       []tilt.FileEvent{{Time: watchAt(1), SeenFiles: []string{"/work/a" + osc}}},
	}}}
	m := watchSetup(t, fake)
	m = press(m, "j", "j", "w")
	raw := m.render()
	if strings.Contains(raw, osc) {
		t.Fatal("the OSC path reached the terminal")
	}
	if rest := sgr.ReplaceAllString(raw, ""); strings.ContainsAny(rest, "\x1b\x07\u009b") {
		t.Fatalf("non-SGR control in output: %q", rest)
	}
	if !strings.Contains(screen(m), "watching  /work") {
		t.Fatalf("panel lacks the path:\n%s", screen(m))
	}
}
