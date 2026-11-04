package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/config"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
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
	// j j moves the cursor to ▼ backend, then to api.
	m = press(m, "j", "j")
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

// wheel sends one wheel notch at x, y after drawing a frame.
func wheel(m Model, x, y int, b tea.MouseButton) Model {
	_ = screen(m)
	m, _ = send(m, tea.MouseWheelMsg{X: x, Y: y, Button: b})
	return m
}

func TestWheelOverListMovesCursor(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = wheel(m, 3, 5, tea.MouseWheelDown)
	m = wheel(m, 3, 5, tea.MouseWheelDown)
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("two notches down did not reach api: %v", r)
	}
	s := screen(m)
	if !strings.Contains(s, "api starting") || strings.Contains(s, "web ready") {
		t.Fatalf("log pane not filtered to api:\n%s", s)
	}
	if !strings.Contains(s, "[follow]") {
		t.Fatalf("wheel over the list scrolled the logs:\n%s", s)
	}
	m = wheel(m, 3, 5, tea.MouseWheelUp)
	if !m.list.OnHeader() {
		t.Fatal("one notch up did not reach the backend header")
	}
	if s := screen(m); !strings.Contains(s, "[follow]") {
		t.Fatalf("wheel up over the list scrolled the logs:\n%s", s)
	}
	m = wheel(m, 3, 5, tea.MouseWheelUp)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("two notches up did not reach All: %+v", m.list.Selected())
	}
	if m.focus != command.FocusList {
		t.Fatalf("wheel changed focus: %v", m.focus)
	}
}

func TestWheelStopsAtEnds(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = wheel(m, 3, 5, tea.MouseWheelUp)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("up at the top moved: %+v", m.list.Selected())
	}
	for range 50 {
		m = wheel(m, 3, 5, tea.MouseWheelDown)
	}
	last := m.list.Selected()
	m = wheel(m, 3, 5, tea.MouseWheelDown)
	if m.list.Selected() != last {
		t.Fatalf("down past the end moved: %+v then %+v", last, m.list.Selected())
	}
}

func TestWheelOverLogsScrollsLogs(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = wheel(m, 100, 5, tea.MouseWheelUp)
	if !m.list.Selected().All {
		t.Fatalf("wheel over the logs moved the cursor: %+v", m.list.Selected())
	}
	if s := screen(m); strings.Contains(s, "[follow]") {
		t.Fatalf("wheel up over the logs did not scroll them:\n%s", s)
	}
	if m.focus != command.FocusList {
		t.Fatalf("wheel changed focus: %v", m.focus)
	}
}

func TestWheelOnFirstLogColumnScrollsLogs(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = wheel(m, listWidth(140), 5, tea.MouseWheelUp)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("wheel on the first log column moved the cursor: %+v", m.list.Selected())
	}
	if s := screen(m); strings.Contains(s, "[follow]") {
		t.Fatalf("wheel up on the first log column did not scroll the logs:\n%s", s)
	}
}

func TestWheelIgnoredUnderOverlayAndNarrowLogs(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m, _ = send(m, PushOverlayMsg{Overlay: testOverlay{}})
	m = wheel(m, 3, 5, tea.MouseWheelDown)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("wheel under an overlay moved the cursor: %+v", m.list.Selected())
	}

	m = mouseSetup(t, 80, 20)
	m = press(m, "enter") // narrow: show the log pane
	m = wheel(m, 3, 5, tea.MouseWheelDown)
	if !m.list.Selected().All || m.list.OnHeader() || m.focus != command.FocusLogs {
		t.Fatalf("wheel on the narrow log pane acted on the list: %+v, focus %v", m.list.Selected(), m.focus)
	}
}

func TestWheelOnFooterDoesNothing(t *testing.T) {
	m := mouseSetup(t, 140, 20)
	m = wheel(m, 3, 19, tea.MouseWheelDown)
	// A header keeps the source of the row above, so check OnHeader too.
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("wheel on the footer moved the cursor: %+v", m.list.Selected())
	}
}

// scrollSetup is setup with only the mouse feature in scroll mode, at w
// x h.
func scrollSetup(t *testing.T, w, h int) Model {
	t.Helper()
	withFeatures(t, mouseFeature)
	m, _ := setup(t, w, h, Deps{WheelList: "scroll"})
	return m
}

// panes splits the screen rows below the title line into the list pane
// part and the log pane part.
func panes(m Model) (list, logs []string) {
	lw := listWidth(m.width)
	for _, l := range strings.Split(screen(m), "\n")[1:] {
		list = append(list, ansi.Cut(l, 0, lw))
		logs = append(logs, ansi.Cut(l, lw, m.width))
	}
	return list, logs
}

// listRows is the number of list rows the pane draws: the lines from the
// one below the title through the one above the status line.
func listRows(t *testing.T, m Model) int {
	t.Helper()
	return listRow(t, m, "● live") - 1
}

func TestWheelModesMatchConfig(t *testing.T) {
	if !slices.Equal(wheelModes, config.WheelModes) {
		t.Fatalf("wheelModes %q, config.WheelModes %q", wheelModes, config.WheelModes)
	}
}

func TestWheelScrollModeScrollsList(t *testing.T) {
	m := scrollSetup(t, 140, 8)
	n := listRows(t, m)
	if n >= 7 {
		t.Fatalf("the list pane draws %d rows, want fewer than 7:\n%s", n, screen(m))
	}
	beforeList, beforeLogs := panes(m)
	m = wheel(m, 3, 2, tea.MouseWheelDown)
	afterList, afterLogs := panes(m)
	for i := range n - 1 {
		if afterList[i] != beforeList[i+1] {
			t.Fatalf("one notch down did not shift the list one line:\nbefore\n%s\nafter\n%s",
				strings.Join(beforeList, "\n"), strings.Join(afterList, "\n"))
		}
	}
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("scroll mode moved the selection: %+v", m.list.Selected())
	}
	if !slices.Equal(afterLogs, beforeLogs) {
		t.Fatalf("scroll mode changed the log pane:\n%s", screen(m))
	}

	for range 50 {
		m = wheel(m, 3, 2, tea.MouseWheelDown)
	}
	last := listRow(t, m, "(Tiltfile)")
	if last != n {
		t.Fatalf("(Tiltfile) on line %d after scrolling down, want the last list line %d:\n%s", last, n, screen(m))
	}
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("scrolling down moved the selection: %+v", m.list.Selected())
	}

	for range 50 {
		m = wheel(m, 3, 2, tea.MouseWheelUp)
	}
	if first := listRow(t, m, "All Resources"); first != 1 {
		t.Fatalf("All Resources on line %d after scrolling up, want 1:\n%s", first, screen(m))
	}
}

func TestWheelScrollModeOverLogsScrollsLogs(t *testing.T) {
	m := scrollSetup(t, 140, 20)
	m = wheel(m, 100, 5, tea.MouseWheelUp)
	if !m.list.Selected().All {
		t.Fatalf("wheel over the logs moved the cursor: %+v", m.list.Selected())
	}
	if s := screen(m); strings.Contains(s, "[follow]") {
		t.Fatalf("wheel up over the logs did not scroll them:\n%s", s)
	}
}

func TestWheelScrollModeClickSelects(t *testing.T) {
	m := scrollSetup(t, 140, 8)
	for range 10 {
		m = wheel(m, 3, 2, tea.MouseWheelDown)
	}
	m = clickAt(m, 3, listRow(t, m, "web"), tea.MouseLeft)
	if r := m.list.SelectedResource(); r == nil || !strings.HasPrefix(r.Name(), "web") {
		t.Fatalf("click after scrolling did not select web: %v", r)
	}
	if s := screen(m); !strings.Contains(s, "web ready") || strings.Contains(s, "api starting") {
		t.Fatalf("log pane not filtered to web:\n%s", s)
	}
}

func TestWheelScrollModeSnapshotKeepsViewKeySnapsBack(t *testing.T) {
	m := scrollSetup(t, 140, 8)
	for range 10 {
		m = wheel(m, 3, 2, tea.MouseWheelDown)
	}
	before, _ := panes(m)
	if strings.Contains(strings.Join(before, "\n"), "All Resources") {
		t.Fatalf("the cursor row is still on screen:\n%s", screen(m))
	}
	m, _ = send(m, snapshot(t, true))
	if after, _ := panes(m); !slices.Equal(after[:listRows(t, m)], before[:listRows(t, m)]) {
		t.Fatalf("a snapshot moved the scrolled view:\n%s", screen(m))
	}
	m = press(m, "j")
	if !m.list.OnHeader() {
		t.Fatalf("j did not move to the backend header: %+v", m.list.Selected())
	}
	_ = listRow(t, m, "▼ backend")
}

func TestSettingMsgChangesWheelMode(t *testing.T) {
	m := mouseSetup(t, 140, 8)
	nm, cmd := m.Update(overlay.SettingMsg{Key: "mouseWheelList", Value: "scroll"})
	m = nm.(Model)
	msgs := drain(cmd)
	if len(msgs) != 1 {
		t.Fatalf("SettingMsg produced %d messages, want one flash", len(msgs))
	}
	f, ok := msgs[0].(flashMsg)
	if !ok || !strings.Contains(f.text, "scroll") || !strings.Contains(f.text, "mouseWheelList") {
		t.Fatalf("flash %+v does not name scroll and mouseWheelList", msgs[0])
	}
	m = wheel(m, 3, 2, tea.MouseWheelDown)
	if !m.list.Selected().All || m.list.OnHeader() {
		t.Fatalf("a notch after switching to scroll moved the selection: %+v", m.list.Selected())
	}

	if cmd, handled := mouseFeature.Update(&m, overlay.SettingMsg{Key: "mouseWheelList", Value: "page"}); cmd != nil || !handled {
		t.Fatalf("unknown value: cmd %v, handled %v, want nil and true", cmd, handled)
	}
	if got := wheelMode(&m); got != "scroll" {
		t.Fatalf("unknown value changed the mode to %q", got)
	}
	if _, handled := mouseFeature.Update(&m, overlay.SettingMsg{Key: "theme", Value: "cursor"}); handled {
		t.Fatal("the mouse feature handled another setting")
	}
	if got := wheelMode(&m); got != "scroll" {
		t.Fatalf("another setting changed the mode to %q", got)
	}

	m, _ = send(m, overlay.SettingMsg{Key: "mouseWheelList", Value: "cursor"})
	m = wheel(m, 3, 2, tea.MouseWheelDown)
	if !m.list.OnHeader() {
		t.Fatalf("a notch after switching back to cursor did not move it: %+v", m.list.Selected())
	}
}
