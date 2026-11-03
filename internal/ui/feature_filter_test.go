package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/resourcelist"
)

// filterSetup is setup with only the filter feature. The snapshot holds
// (Tiltfile) and web, both healthy, and api with a build error.
func filterSetup(t *testing.T, w, h int) Model {
	t.Helper()
	withFeatures(t, filterFeature)
	m, _ := setup(t, w, h, Deps{})
	return m
}

func TestFilterCyclesWithF(t *testing.T) {
	m := filterSetup(t, 140, 20)
	want := []resourcelist.Filter{resourcelist.FilterErrors, resourcelist.FilterInProgress, resourcelist.FilterHealthy, resourcelist.FilterAll}
	for i, f := range want {
		m = press(m, "f")
		if m.list.Filter() != f {
			t.Fatalf("press %d: filter = %v, want %v", i+1, m.list.Filter(), f)
		}
	}
}

func TestFilterHeaderAndCounts(t *testing.T) {
	m := filterSetup(t, 140, 20)
	m = press(m, "f")
	s := screen(m)
	if !strings.Contains(s, "Resources  ✗ 1  ✓ 2/3  [errors]") {
		t.Fatalf("header lacks the filter or the full counts:\n%s", s)
	}
	if !strings.Contains(s, "●  api") || strings.Contains(s, "●  web") || strings.Contains(s, "(Tiltfile)") {
		t.Fatalf("errors filter shows the wrong resources:\n%s", s)
	}
	m = press(m, "f")
	if s := screen(m); !strings.Contains(s, "[in progress]") || strings.Contains(s, "●  api") {
		t.Fatalf("in progress filter:\n%s", s)
	}
}

func TestFilterKeepsSelectionAndLogs(t *testing.T) {
	m := filterSetup(t, 140, 20)
	m = press(m, "j", "j", "j", "j") // All, ▼ backend, api, ▼ frontend, web
	src := m.list.Selected()
	if src.Resource == "" {
		t.Fatalf("cursor not on web: %+v", src)
	}
	m = press(m, "f")
	if m.list.Selected() != src || m.logs.Source() != src {
		t.Fatalf("filter changed the selection or logs: list %+v, logs %+v", m.list.Selected(), m.logs.Source())
	}
	if s := screen(m); !strings.Contains(s, "web (filtered)") || !strings.Contains(s, "web ready") {
		t.Fatalf("selected resource not kept with its logs:\n%s", s)
	}
	m = press(m, "k")
	if s := screen(m); strings.Contains(s, "web (filtered)") {
		t.Fatalf("filtered resource stayed after the cursor left:\n%s", s)
	}
	if m.list.Selected() != (logstore.Source{Resource: "api"}) {
		t.Fatalf("Selected = %+v, want api", m.list.Selected())
	}
}

func TestFilterEscClears(t *testing.T) {
	for _, w := range []int{140, 80} {
		m := filterSetup(t, w, 20)
		m = press(m, "f", "esc")
		if m.list.Filter() != resourcelist.FilterAll {
			t.Fatalf("width %d: esc left filter %v", w, m.list.Filter())
		}
		if s := screen(m); strings.Contains(s, "[errors]") || !strings.Contains(s, "(Tiltfile)") {
			t.Fatalf("width %d: list still filtered:\n%s", w, s)
		}
	}
}

func TestFilterKeysOnlyInList(t *testing.T) {
	m := filterSetup(t, 140, 20)
	m = press(m, "tab", "f")
	if m.list.Filter() != resourcelist.FilterAll {
		t.Fatal("f changed the filter with the log pane focused")
	}
}

func TestFilterFooterAndHelp(t *testing.T) {
	m := filterSetup(t, 140, 30)
	if s := screen(m); !strings.Contains(s, "r trigger · f filter · tab switch") {
		t.Fatalf("footer lacks the filter hint:\n%s", s)
	}
	m = press(m, "f", "?")
	for _, want := range []string{"status filter", "clear status filter"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("help lacks %q:\n%s", want, screen(m))
		}
	}
}

func TestFilterGolden(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		keys []string
	}{
		{"filter_errors_140", 140, 20, []string{"f"}},
		{"filter_kept_80", 80, 20, []string{"j", "j", "j", "j", "f"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := press(filterSetup(t, tc.w, tc.h), tc.keys...)
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
