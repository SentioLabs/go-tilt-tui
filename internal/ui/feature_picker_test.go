package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// pickerSetup is setup with only the picker feature. The snapshot holds
// (Tiltfile), api in group backend and web in group frontend.
func pickerSetup(t *testing.T, w, h int) Model {
	t.Helper()
	withFeatures(t, pickerFeature)
	m, _ := setup(t, w, h, Deps{})
	return m
}

// pickerPress is press with a real space key. The shared key helper
// would send "space" as five typed letters.
func pickerPress(m Model, keys ...string) Model {
	for _, k := range keys {
		msg := key(k)
		if k == "space" {
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		}
		m, _ = send(m, msg)
	}
	return m
}

func TestPickerSpaceOpensAndEnterSelects(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	m = pickerPress(m, "space")
	if len(m.overlays) != 1 {
		t.Fatal("space did not open the picker")
	}
	if _, ok := m.overlays[0].(*overlay.Picker); !ok {
		t.Fatalf("top overlay is %T", m.overlays[0])
	}
	m = pickerPress(m, "w", "e", "enter")
	if len(m.overlays) != 0 {
		t.Fatal("enter did not close the picker")
	}
	r := m.list.SelectedResource()
	if r == nil || !strings.HasPrefix(r.Name(), "web") {
		t.Fatalf("SelectedResource = %v, want web", r)
	}
	if m.logs.Source() != m.list.Selected() {
		t.Fatalf("log source %+v, list %+v", m.logs.Source(), m.list.Selected())
	}
	if s := screen(m); !strings.Contains(s, "web ready") || strings.Contains(s, "api starting") {
		t.Fatalf("log pane does not show web alone:\n%s", s)
	}
}

func TestPickerExpandsCollapsedGroup(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	m = pickerPress(m, "c") // collapse every group
	m = pickerPress(m, "space", "a", "p", "i", "enter")
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("SelectedResource = %v, want api", r)
	}
	if s := screen(m); !strings.Contains(s, "▼ backend") || !strings.Contains(s, "▶ frontend") {
		t.Fatalf("picking api did not expand only its group:\n%s", s)
	}
}

func TestPickerEscChangesNothing(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	before := m.list.Selected()
	m = pickerPress(m, "space", "a", "esc")
	if len(m.overlays) != 0 || m.list.Selected() != before {
		t.Fatalf("esc: overlays %d, selected %+v", len(m.overlays), m.list.Selected())
	}
}

func TestPickerKeysGoToQuery(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	m = pickerPress(m, "space", "q", "j", "space")
	m, _ = send(m, tea.PasteMsg{Content: "x"})
	p, ok := m.overlays[len(m.overlays)-1].(*overlay.Picker)
	if !ok || p.Value() != "qj x" {
		t.Fatalf("picker query = %q, want %q", p.Value(), "qj x")
	}
}

func TestPickerFooterHint(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	if s := screen(m); !strings.Contains(s, "r trigger · space resources · tab switch") {
		t.Fatalf("footer lacks the picker hint:\n%s", s)
	}
}

func TestPickerOnlySGRReachesTheTerminal(t *testing.T) {
	m := pickerSetup(t, 140, 20)
	m = pickerPress(m, "space")
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07\u009b") {
		t.Fatalf("non-SGR control in output: %q", rest)
	}
}

func TestPickerGolden(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		keys []string
	}{
		{"picker_140", 140, 20, []string{"space"}},
		{"picker_query_80", 80, 20, []string{"space", "e"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := pickerPress(pickerSetup(t, tc.w, tc.h), tc.keys...)
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
