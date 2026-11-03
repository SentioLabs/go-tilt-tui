package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// infoDeps is a Deps.Dependencies that returns deps and err and records
// each call.
type infoDeps struct {
	deps        map[string][]string
	err         error
	calls       int
	hadDeadline bool
}

func (f *infoDeps) fetch(ctx context.Context) (map[string][]string, error) {
	f.calls++
	_, f.hadDeadline = ctx.Deadline()
	return f.deps, f.err
}

// infoSetup is setup with only the info feature, d as the deps and the
// cursor on api.
func infoSetup(t *testing.T, w, h int, d Deps) Model {
	t.Helper()
	withFeatures(t, infoFeature)
	m, _ := setup(t, w, h, d)
	m = press(m, "j", "j") // All, ▼ backend, api
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("cursor not on api: %v", r)
	}
	return m
}

func TestInfoAllRowDoesNothing(t *testing.T) {
	withFeatures(t, infoFeature)
	f := &infoDeps{}
	m, _ := setup(t, 140, 20, Deps{Dependencies: f.fetch})
	m = press(m, "i")
	if len(m.overlays) != 0 {
		t.Fatalf("i on All Resources pushed %T", m.overlays[0])
	}
	if f.calls != 0 {
		t.Fatal("i on All Resources fetched dependencies")
	}
}

func TestInfoShowsDependencies(t *testing.T) {
	f := &infoDeps{deps: map[string][]string{"api": {"db"}}}
	m := infoSetup(t, 140, 30, Deps{Dependencies: f.fetch})
	m = press(m, "i")
	if len(m.overlays) != 1 {
		t.Fatalf("i pushed %d overlays", len(m.overlays))
	}
	ov, ok := m.overlays[0].(*overlay.Info)
	if !ok {
		t.Fatalf("top overlay is %T", m.overlays[0])
	}
	if ov.Resource() != "api" {
		t.Fatalf("panel is for %q", ov.Resource())
	}
	if s := screen(m); !strings.Contains(s, "└─ db") {
		t.Fatalf("no dependency tree:\n%s", s)
	}
	if f.calls != 1 || !f.hadDeadline {
		t.Fatalf("fetch called %d times, deadline %v", f.calls, f.hadDeadline)
	}
}

func TestInfoWithoutDependencies(t *testing.T) {
	m := infoSetup(t, 140, 30, Deps{})
	m = press(m, "i")
	if len(m.overlays) != 1 {
		t.Fatalf("i pushed %d overlays", len(m.overlays))
	}
	if s := screen(m); !strings.Contains(s, "dependency info unavailable") {
		t.Fatalf("no unavailable message:\n%s", s)
	}
}

func TestInfoTooLargeKeepsRunning(t *testing.T) {
	f := &infoDeps{err: fmt.Errorf("x: %w", tilt.ErrTooLarge)}
	m := infoSetup(t, 140, 30, Deps{Dependencies: f.fetch})
	m = press(m, "i")
	if s := screen(m); !strings.Contains(s, "engine dump too large (over 64 MiB)") {
		t.Fatalf("no too-large message:\n%s", s)
	}
	m = press(m, "j")
	if len(m.overlays) != 1 {
		t.Fatal("j closed the panel")
	}
	m = press(m, "esc")
	if len(m.overlays) != 0 {
		t.Fatal("esc did not close the panel")
	}
	m = press(m, "k")
	if r := m.list.SelectedResource(); r != nil && r.Name() == "api" {
		t.Fatal("k after closing did not reach the list")
	}
}

func TestInfoGolden(t *testing.T) {
	f := &infoDeps{deps: map[string][]string{"api": {"db"}, "web": {"api"}, "db": nil}}
	withFeatures(t, infoFeature)
	m, _ := setup(t, 140, 30, Deps{Dependencies: f.fetch})
	m = press(m, "j", "j", "i")
	checkFrame(t, m, 140, 30)
	got := screen(m)
	path := filepath.Join("testdata", "info_140.golden")
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

func TestInfoOnlySGRReachesTheTerminal(t *testing.T) {
	f := &infoDeps{deps: map[string][]string{"api": {"d\x1b]52;c;ZXZpbA==\x07b"}}}
	m := infoSetup(t, 140, 30, Deps{Dependencies: f.fetch})
	m = press(m, "i")
	if len(m.overlays) != 1 {
		t.Fatal("no info panel")
	}
	out := m.render()
	if strings.Contains(out, "\x1b]") {
		t.Fatalf("OSC in output: %q", out)
	}
	if rest := sgr.ReplaceAllString(out, ""); strings.ContainsAny(rest, "\x1b\x07\u009b") {
		t.Fatalf("non-SGR control in output: %q", rest)
	}
}
