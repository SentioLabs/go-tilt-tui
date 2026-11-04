package overlay

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func testSettings(t *testing.T) Overlay {
	t.Helper()
	return NewSettings(formTheme(t), []Setting{
		{Key: "a", Label: "Alpha", Options: []string{"x", "y", "z"}, Help: []string{"help x", "help y", "help z"}, Value: "x"},
		{Key: "b", Label: "Beta", Options: []string{"p", "q"}, Help: []string{"help p", "help q"}, Value: "p"},
	})
}

// settingChange returns the SettingMsg in msgs and whether there was one.
func settingChange(msgs []tea.Msg) (SettingMsg, bool) {
	for _, m := range msgs {
		if s, ok := m.(SettingMsg); ok {
			return s, true
		}
	}
	return SettingMsg{}, false
}

func TestSettingsTitle(t *testing.T) {
	if got := testSettings(t).Title(); got != "Settings" {
		t.Fatalf("title = %q", got)
	}
}

func TestSettingsViewShowsValues(t *testing.T) {
	o := testSettings(t)
	raw := o.View(80, 20)
	for _, l := range strings.Split(raw, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Fatalf("line %q is %d wide", l, w)
		}
	}
	got := ansi.Strip(raw)
	for _, want := range []string{"› Alpha  ‹ x ›", "  Beta  ‹ p ›", "help x", "esc close"} {
		if !strings.Contains(got, want) {
			t.Fatalf("view lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "help p") {
		t.Fatalf("view shows help for a row without the cursor:\n%s", got)
	}
}

func TestSettingsCycleForward(t *testing.T) {
	for _, k := range []string{"enter", "space", "l", "right"} {
		o, msgs := formPress(testSettings(t), k)
		if got, ok := settingChange(msgs); !ok || got != (SettingMsg{Key: "a", Value: "y"}) {
			t.Fatalf("%s: msgs %v", k, msgs)
		}
		if v := ansi.Strip(o.View(80, 20)); !strings.Contains(v, "‹ y ›") || !strings.Contains(v, "help y") {
			t.Fatalf("%s: view did not update:\n%s", k, v)
		}
	}
	o, _ := formPress(testSettings(t), "enter", "enter")
	_, msgs := formPress(o, "enter")
	if got, ok := settingChange(msgs); !ok || got.Value != "x" {
		t.Fatalf("forward from z did not wrap: %v", msgs)
	}
}

func TestSettingsCycleBack(t *testing.T) {
	for _, k := range []string{"h", "left"} {
		o, msgs := formPress(testSettings(t), k)
		if got, ok := settingChange(msgs); !ok || got != (SettingMsg{Key: "a", Value: "z"}) {
			t.Fatalf("%s: msgs %v", k, msgs)
		}
		_, msgs = formPress(o, k)
		if got, ok := settingChange(msgs); !ok || got.Value != "y" {
			t.Fatalf("%s from z: msgs %v", k, msgs)
		}
	}
}

func TestSettingsMoveRows(t *testing.T) {
	for _, down := range []string{"j", "down"} {
		o, _ := formPress(testSettings(t), down, down)
		if v := ansi.Strip(o.View(80, 20)); !strings.Contains(v, "› Beta") || !strings.Contains(v, "help p") {
			t.Fatalf("%s: cursor not on Beta:\n%s", down, v)
		}
		_, msgs := formPress(o, "enter")
		if got, ok := settingChange(msgs); !ok || got != (SettingMsg{Key: "b", Value: "q"}) {
			t.Fatalf("%s: msgs %v", down, msgs)
		}
	}
	for _, up := range []string{"k", "up"} {
		o, _ := formPress(testSettings(t), "j", up, up)
		if v := ansi.Strip(o.View(80, 20)); !strings.Contains(v, "› Alpha") {
			t.Fatalf("%s: cursor not on Alpha:\n%s", up, v)
		}
	}
}

func TestSettingsClose(t *testing.T) {
	for _, k := range []string{"esc", "q"} {
		_, msgs := formPress(testSettings(t), k)
		if len(msgs) != 1 || msgs[0] != (CloseMsg{}) {
			t.Fatalf("%s: msgs %v", k, msgs)
		}
	}
}

func TestSettingsTermsafe(t *testing.T) {
	o := NewSettings(formTheme(t), []Setting{
		{Key: "a", Label: "Al\x1b[31mpha", Options: []string{"x\x1b[31m"}, Help: []string{"he\x1b[31mlp"}, Value: "x\x1b[31m"},
	})
	if v := ansi.Strip(o.View(80, 20)); strings.ContainsRune(v, '\x1b') {
		t.Fatalf("view has an ESC byte: %q", v)
	}
	if v := o.View(80, 20); strings.Contains(v, "\x1b[31m") {
		t.Fatalf("view passes the raw sequence through: %q", v)
	}
}

func TestSettingsUnknownValue(t *testing.T) {
	o := NewSettings(formTheme(t), []Setting{
		{Key: "a", Label: "Alpha", Options: []string{"x", "y"}, Value: "nope"},
	})
	if v := ansi.Strip(o.View(80, 20)); !strings.Contains(v, "‹ x ›") {
		t.Fatalf("view does not show the first option:\n%s", v)
	}
	_, msgs := formPress(o, "enter")
	if got, ok := settingChange(msgs); !ok || got.Value != "y" {
		t.Fatalf("step from an unknown value: %v", msgs)
	}
}
