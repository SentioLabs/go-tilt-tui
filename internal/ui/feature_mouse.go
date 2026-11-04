package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// mouseFeature handles left clicks. A click in a pane focuses it, and a
// click on a list row selects that row. It keeps no state: the list
// remembers the scroll offset of the frame on screen.
var mouseFeature = Feature{
	Name: "mouse",
	Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
		c, ok := msg.(tea.MouseClickMsg)
		if !ok || c.Button != tea.MouseLeft {
			return nil, false
		}
		m.click(c.X, c.Y)
		return nil, true
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
