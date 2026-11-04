package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// wheelModes matches config.WheelModes, a test keeps them equal.
var wheelModes = []string{"cursor", "scroll"}

type mouseState struct{ wheel string } // one of wheelModes

// mouseFeature handles left clicks and wheel notches. A click in a pane
// focuses it, and a click on a list row selects that row. A wheel notch
// over the list moves the list cursor one row in cursor mode, and in
// scroll mode it scrolls the list view one line and keeps the selection.
// The mode starts from Deps.WheelList, and a SettingMsg for
// mouseWheelList changes it for the session. The list remembers the
// scroll offset of the frame on screen.
var mouseFeature = Feature{
	Name: "mouse",
	Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
		switch msg := msg.(type) {
		case tea.MouseClickMsg:
			if msg.Button != tea.MouseLeft {
				return nil, false
			}
			m.click(msg.X, msg.Y)
			return nil, true
		case tea.MouseWheelMsg:
			return nil, m.wheel(msg)
		case overlay.SettingMsg:
			if msg.Key != "mouseWheelList" {
				return nil, false
			}
			if !slices.Contains(wheelModes, msg.Value) {
				return nil, true
			}
			mouse(m).wheel = msg.Value
			return m.flashCmd("mouse wheel over list: "+msg.Value+" (this session, set mouseWheelList in config.toml to keep it)", 0), true
		}
		return nil, false
	},
}

func init() { register(mouseFeature) }

// mouse returns the mouse feature's state, with the wheel mode taken from
// Deps.WheelList on first use.
func mouse(m *Model) *mouseState {
	return featureState(m, "mouse", func() *mouseState {
		if slices.Contains(wheelModes, m.d.WheelList) {
			return &mouseState{wheel: m.d.WheelList}
		}
		return &mouseState{wheel: wheelModes[0]}
	})
}

// wheelMode returns the wheel mode in use, one of wheelModes.
func wheelMode(m *Model) string { return mouse(m).wheel }

// wheelSetting is the settings pane row for the wheel mode.
func wheelSetting(m *Model) overlay.Setting {
	return overlay.Setting{
		Key:     "mouseWheelList",
		Label:   "Mouse wheel over list",
		Options: wheelModes,
		Help:    []string{"moves the selection", "scrolls the list, the selection stays"},
		Value:   wheelMode(m),
	}
}

// click acts on a left click at cell x, y.
func (m *Model) click(x, y int) {
	if len(m.overlays) > 0 || y < 0 || y >= m.height-1 {
		return
	}
	if m.narrow() {
		if m.focus == command.FocusLogs {
			return
		}
	} else if x >= listWidth(m.width) {
		m.focus = command.FocusLogs
		return
	} else {
		m.focus = command.FocusList
	}
	m.list.ClickRow(y - 1)
}

// wheel acts on a wheel notch over the list pane and reports whether it
// did. Cursor mode moves the list cursor one row. Scroll mode scrolls the
// list view one line and leaves the cursor alone. Any other notch is left
// to the model, which scrolls the logs.
func (m *Model) wheel(w tea.MouseWheelMsg) bool {
	var d int
	switch w.Button {
	case tea.MouseWheelUp:
		d = -1
	case tea.MouseWheelDown:
		d = 1
	default:
		return false
	}
	if len(m.overlays) > 0 || w.Y < 0 || w.Y >= m.height-1 {
		return false
	}
	if m.narrow() {
		if m.focus == command.FocusLogs {
			return false
		}
	} else if w.X >= listWidth(m.width) {
		return false
	}
	if wheelMode(m) == "scroll" {
		m.list.ScrollView(d)
	} else {
		m.list.MoveCursor(d)
	}
	return true
}
