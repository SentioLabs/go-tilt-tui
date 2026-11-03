package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// pickerFeature is the resource picker: space opens it and enter moves
// the list cursor to the picked resource. The picker overlay holds all of
// its state, so this feature keeps none.
var pickerFeature = Feature{
	Name:     "picker",
	Commands: pickerCommands,
	Update:   pickerUpdate,
	Hints:    []Hint{{ID: "picker", Label: "resources"}},
}

func init() { register(pickerFeature) }

func pickerCommands(m *Model) []command.Command {
	return []command.Command{
		{ID: "picker", Title: "go to resource", Keys: []string{"space"}, Scope: command.ScopeGlobal,
			Run: func(command.Context) tea.Cmd {
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewPicker(m.d.Theme, m.store.Resources())})
			}},
	}
}

func pickerUpdate(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case overlay.PickMsg:
		if !m.list.Select(msg.Name) {
			m.setFlash("no resource "+termsafe.Text(msg.Name), m.d.Now(), flashFor)
			return nil, true
		}
		// Point the log view at the pick now. Update also does this
		// after every message.
		m.syncSource()
		return nil, true
	case tea.PasteMsg:
		// The root model routes only key presses to overlays.
		n := len(m.overlays)
		if n == 0 {
			return nil, false
		}
		if _, ok := m.overlays[n-1].(*overlay.Picker); !ok {
			return nil, false
		}
		top, cmd := m.overlays[n-1].Update(msg)
		m.overlays = append(slices.Clone(m.overlays[:n-1]), top)
		return cmd, true
	}
	return nil, false
}
