package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// paletteFeature is the command palette. The overlay holds its state.
var paletteFeature = Feature{
	Name: "palette",
	Commands: func(m *Model) []command.Command {
		return []command.Command{{
			ID: "palette", Title: "command palette", Keys: []string{":"}, Scope: command.ScopeGlobal,
			Run: func(ctx command.Context) tea.Cmd {
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewPalette(m.d.Theme, paletteCommands(m, ctx))})
			},
		}}
	},
	Update: paletteUpdate,
	Hints:  []Hint{{ID: "palette", Label: "commands"}},
}

func init() { register(paletteFeature) }

// paletteCommands is every command that would do something if run now.
func paletteCommands(m *Model, ctx command.Context) []command.Command {
	var out []command.Command
	for _, c := range m.commands() {
		if c.Run == nil || c.ID == "palette" || !inScope(c.Scope, ctx.Focus) {
			continue
		}
		if c.Enabled != nil && !c.Enabled(ctx) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// paletteUpdate pops the palette and runs the picked command in one step,
// so an overlay that command pushes lands above whatever was under the
// palette. It also forwards pastes, because the root model routes only
// key presses to overlays.
func paletteUpdate(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	n := len(m.overlays)
	onTop := false
	if n > 0 {
		_, onTop = m.overlays[n-1].(*overlay.Palette)
	}
	switch msg := msg.(type) {
	case overlay.PaletteRunMsg:
		if onTop {
			m.overlays = slices.Clip(m.overlays[:n-1])
		}
		// Look the command up again: the state may have changed since
		// the palette opened.
		ctx := m.context()
		for _, c := range paletteCommands(m, ctx) {
			if c.ID == msg.ID {
				return c.Run(ctx), true
			}
		}
		m.setFlash("command not available", m.d.Now(), flashFor)
		return nil, true
	case tea.PasteMsg:
		if !onTop {
			return nil, false
		}
		top, cmd := m.overlays[n-1].Update(msg)
		m.overlays = append(slices.Clone(m.overlays[:n-1]), top)
		return cmd, true
	}
	return nil, false
}
