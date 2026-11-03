package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// Feature adds commands and message handling to the root model. P2 and P3
// files in package ui call register from init(), so they never edit
// model.go.
type Feature struct {
	Name     string
	Commands func(m *Model) []command.Command
	Update   func(m *Model, msg tea.Msg) (cmd tea.Cmd, handled bool)
}

var features []Feature

func register(f Feature) { features = append(features, f) }

// PushOverlayMsg asks the root model to push an overlay on the stack.
type PushOverlayMsg struct{ Overlay overlay.Overlay }
