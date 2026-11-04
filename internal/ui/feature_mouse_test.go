package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// mouseSetup is setup with only the mouse feature, at w x h.
func mouseSetup(t *testing.T, w, h int) Model {
	t.Helper()
	withFeatures(t, mouseFeature)
	m, _ := setup(t, w, h, Deps{})
	return m
}

// clickAt sends a click at x, y after drawing a frame, the way the
// terminal only reports clicks on a frame already on screen.
func clickAt(m Model, x, y int, b tea.MouseButton) Model {
	_ = screen(m)
	m, _ = send(m, tea.MouseClickMsg{X: x, Y: y, Button: b})
	return m
}

// listRow returns the screen row whose list pane part contains text.
func listRow(t *testing.T, m Model, text string) int {
	t.Helper()
	lw := m.width
	if !m.narrow() {
		lw = listWidth(m.width)
	}
	for i, l := range strings.Split(screen(m), "\n") {
		if i > 0 && strings.Contains(ansi.Cut(l, 0, lw), text) {
			return i
		}
	}
	t.Fatalf("no list row holds %q:\n%s", text, screen(m))
	return -1
}

func TestClickSelectsResource(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = clickAt(m, 3, listRow(t, m, "api"), tea.MouseLeft)
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("click did not select api: %v", r)
	}
	s := screen(m)
	if !strings.Contains(s, "api starting") || strings.Contains(s, "web ready") {
		t.Fatalf("log pane not filtered to api:\n%s", s)
	}
}

func TestClickHeaderSelectsThenToggles(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	y := listRow(t, m, "backend")
	m = clickAt(m, 3, y, tea.MouseLeft)
	if !m.list.OnHeader() || !m.list.Selected().All {
		t.Fatalf("first click: OnHeader %v, Selected %+v", m.list.OnHeader(), m.list.Selected())
	}
	m = clickAt(m, 3, y, tea.MouseLeft)
	if s := screen(m); !strings.Contains(s, "▶ backend") {
		t.Fatalf("second click did not collapse:\n%s", s)
	}
	m = clickAt(m, 3, y, tea.MouseLeft)
	if s := screen(m); !strings.Contains(s, "▼ backend") {
		t.Fatalf("third click did not expand:\n%s", s)
	}
}

func TestClickOutsideRowsDoesNothing(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = press(m, "j", "j") // ▼ backend, api
	for _, y := range []int{0, 15, 17, 18, 19} { // title, empty line, status line, bottom border, footer
		m = clickAt(m, 3, y, tea.MouseLeft)
		if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
			t.Fatalf("click at y=%d moved the cursor: %v\n%s", y, r, screen(m))
		}
		if m.focus != command.FocusList {
			t.Fatalf("click at y=%d changed focus", y)
		}
	}
}

func TestClickSetsFocus(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = clickAt(m, 100, 5, tea.MouseLeft)
	if m.focus != command.FocusLogs {
		t.Fatalf("click in log pane: focus %v", m.focus)
	}
	m = clickAt(m, 3, 0, tea.MouseLeft)
	if m.focus != command.FocusList {
		t.Fatalf("click in list pane: focus %v", m.focus)
	}
}

func TestClickIgnoredUnderOverlayAndRightButton(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	y := listRow(t, m, "api")
	m = clickAt(m, 3, y, tea.MouseRight)
	if m.list.SelectedResource() != nil {
		t.Fatal("right click selected a row")
	}
	m, _ = send(m, PushOverlayMsg{Overlay: testOverlay{}})
	m = clickAt(m, 3, y, tea.MouseLeft)
	m = clickAt(m, 100, 5, tea.MouseLeft)
	if m.list.SelectedResource() != nil || m.focus != command.FocusList {
		t.Fatalf("click under an overlay acted: %v, focus %v", m.list.SelectedResource(), m.focus)
	}
}

func TestClickNarrowLogPaneDoesNothing(t *testing.T) {
	m := mouseSetup(t, 80, 20)
	y := listRow(t, m, "api")
	m = press(m, "enter") // narrow: show the log pane
	if m.focus != command.FocusLogs {
		t.Fatal("enter did not show the log pane")
	}
	m = clickAt(m, 3, y, tea.MouseLeft)
	if m.focus != command.FocusLogs || m.list.SelectedResource() != nil {
		t.Fatalf("click on the narrow log pane acted: focus %v, %v", m.focus, m.list.SelectedResource())
	}
	m = press(m, "esc")
	m = clickAt(m, 3, y, tea.MouseLeft)
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("narrow list click did not select api: %v", r)
	}
}
