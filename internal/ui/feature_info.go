package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// depsTimeout bounds one Deps.Dependencies call.
const depsTimeout = 15 * time.Second

// infoDepsMsg carries a dependency fetch back to the panel that asked.
type infoDepsMsg struct {
	target *overlay.Info
	deps   map[string][]string
	err    error
}

// infoFeature is the resource info panel. The panel holds its own
// state, so this feature keeps none.
var infoFeature = Feature{
	Name: "info",
	Commands: func(m *Model) []command.Command {
		return []command.Command{{
			ID: "info", Title: "resource info", Keys: []string{"i"}, Scope: command.ScopeGlobal,
			Enabled: func(ctx command.Context) bool { return ctx.Resource != nil },
			Run:     func(ctx command.Context) tea.Cmd { return m.openInfo(*ctx.Resource) },
		}}
	},
	Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
		if r, ok := msg.(infoDepsMsg); ok {
			r.target.SetDeps(r.deps, r.err)
			return nil, true
		}
		return nil, false
	},
	Hints: []Hint{{ID: "info", Label: "info"}},
}

func init() { register(infoFeature) }

// openInfo pushes the panel for r and starts loading its dependencies.
func (m *Model) openInfo(r tilt.Resource) tea.Cmd {
	ov := overlay.NewInfo(m.d.Theme, r)
	push := msgCmd(PushOverlayMsg{Overlay: ov})
	fetch := m.d.Dependencies
	if fetch == nil {
		ov.SetDeps(nil, overlay.ErrDepsUnavailable)
		return push
	}
	return tea.Batch(push, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), depsTimeout)
		defer cancel()
		deps, err := fetch(ctx)
		return infoDepsMsg{target: ov, deps: deps, err: err}
	})
}
