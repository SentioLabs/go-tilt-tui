package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// watchesTimeout bounds one Deps.FileWatches call.
const watchesTimeout = 15 * time.Second

// watchesMsg carries a file-watch fetch back to the panel that asked.
type watchesMsg struct {
	target *overlay.Watches
	ws     []tilt.FileWatch
	err    error
}

// watchFeature is the file-watch panel. The panel holds its own state.
var watchFeature = Feature{
	Name: "watch",
	Commands: func(m *Model) []command.Command {
		return []command.Command{{
			ID: "watches", Title: "file-watch info", Keys: []string{"w"}, Scope: command.ScopeGlobal,
			Run: func(ctx command.Context) tea.Cmd {
				name := ""
				if ctx.Resource != nil {
					name = ctx.Resource.Name()
				}
				return m.openWatches(name)
			},
		}}
	},
	Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
		if r, ok := msg.(watchesMsg); ok {
			r.target.SetWatches(r.ws, r.err)
			return nil, true
		}
		return nil, false
	},
}

func init() { register(watchFeature) }

// openWatches pushes the panel for resource ("" for all) and starts
// loading. Without Deps.FileWatches the panel says it needs a local Tilt.
func (m *Model) openWatches(resource string) tea.Cmd {
	ov := overlay.NewWatches(m.d.Theme, resource)
	push := msgCmd(PushOverlayMsg{Overlay: ov})
	fetch := m.d.FileWatches
	if fetch == nil {
		ov.SetWatches(nil, overlay.ErrNeedsLocalTilt)
		return push
	}
	return tea.Batch(push, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), watchesTimeout)
		defer cancel()
		ws, err := fetch(ctx)
		return watchesMsg{target: ov, ws: ws, err: err}
	})
}
