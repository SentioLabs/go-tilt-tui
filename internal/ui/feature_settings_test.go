package ui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// settingsSetup is setup with the mouse and settings features and d as
// the deps.
func settingsSetup(t *testing.T, w, h int, d Deps) Model {
	t.Helper()
	withFeatures(t, mouseFeature, settingsFeature)
	m, _ := setup(t, w, h, d)
	return m
}

// settingsView returns the top overlay's view, or fails unless it is the
// settings pane.
func settingsView(t *testing.T, m Model) string {
	t.Helper()
	if len(m.overlays) != 1 {
		t.Fatalf("%d overlays open, want the settings pane", len(m.overlays))
	}
	top := m.overlays[0]
	if top.Title() != "Settings" {
		t.Fatalf("top overlay is %q, want Settings", top.Title())
	}
	return ansi.Strip(top.View(80, 10))
}

func TestSettingsKeyOpensPane(t *testing.T) {
	m := settingsSetup(t, 140, 20, Deps{})
	m = press(m, ",")
	v := settingsView(t, m)
	if !strings.Contains(v, "Mouse wheel over list") || !strings.Contains(v, "‹ cursor ›") {
		t.Fatalf("settings pane lacks the wheel row in cursor mode:\n%s", v)
	}
}

func TestSettingsPaneShowsScrollMode(t *testing.T) {
	m := settingsSetup(t, 140, 20, Deps{WheelList: "scroll"})
	m = press(m, ",")
	if v := settingsView(t, m); !strings.Contains(v, "‹ scroll ›") {
		t.Fatalf("settings pane does not show scroll:\n%s", v)
	}
}

func TestSettingsEnterSwitchesWheelMode(t *testing.T) {
	m := settingsSetup(t, 140, 8, Deps{})
	m = press(m, ",")
	m, msgs := send(m, key("enter"))
	want := overlay.SettingMsg{Key: "mouseWheelList", Value: "scroll"}
	if !slices.Contains(msgs, tea.Msg(want)) {
		t.Fatalf("enter sent %v, want %+v", msgs, want)
	}
	if v := settingsView(t, m); !strings.Contains(v, "‹ scroll ›") {
		t.Fatalf("settings pane does not show scroll after enter:\n%s", v)
	}

	m = press(m, "esc")
	if len(m.overlays) != 0 {
		t.Fatal("esc did not close the settings pane")
	}
	m = wheel(m, 3, 2, tea.MouseWheelDown)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("a notch after enter moved the selection: %+v", m.list.Selected())
	}
	if line := listRow(t, m, "▼ backend"); line != 1 {
		t.Fatalf("a notch after enter did not scroll the list, ▼ backend on line %d:\n%s", line, screen(m))
	}
}

func TestSettingsChangeFlashesConfigKey(t *testing.T) {
	m := settingsSetup(t, 140, 20, Deps{})
	m = press(m, ",")
	m = paletteSendAll(m, key("enter"))
	want := "mouse wheel over list: scroll (this session, set mouseWheelList in config.toml to keep it)"
	if s := screen(m); !strings.Contains(s, want) {
		t.Fatalf("footer lacks %q:\n%s", want, s)
	}
}

func TestSettingsInPaletteAndHelp(t *testing.T) {
	withFeatures(t, settingsFeature, paletteFeature)
	m, _ := setup(t, 140, 40, Deps{})
	m = press(m, ":")
	if got := paletteListed(t, m); !slices.Contains(got, "settings") {
		t.Fatalf("palette lacks settings: %q", got)
	}

	m, _ = setup(t, 140, 40, Deps{})
	m = press(m, "?")
	if s := screen(m); !regexp.MustCompile(`,\s+settings`).MatchString(s) {
		t.Fatalf("help lacks \", settings\":\n%s", s)
	}
}
