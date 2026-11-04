package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// mouseFeature handles left clicks and wheel notches. A click in a pane
// focuses it, and a click on a list row selects that row. A wheel notch
// over the list moves the list cursor one row. It keeps no state: the list
// remembers the scroll offset of the frame on screen.
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
		}
		return nil, false
	},
}

func init() { register(mouseFeature) }

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

// wheel moves the list cursor one row for a wheel notch over the list
// pane and reports whether it did. Any other notch is left to the model,
// which scrolls the logs.
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
	m.list.MoveCursor(d)
	return true
}
