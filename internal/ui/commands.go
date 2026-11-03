package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// triggerTimeout bounds one Trigger call.
const triggerTimeout = 15 * time.Second

// commands returns the root model's commands, then every feature's. The
// commands with a nil Run document keys that the focused pane handles.
func (m *Model) commands() []command.Command {
	cmds := []command.Command{
		{ID: "quit", Title: "quit", Keys: []string{"q"}, Scope: command.ScopeGlobal,
			Run: func(command.Context) tea.Cmd { return tea.Quit }},
		{ID: "switch-pane", Title: "switch pane", Keys: []string{"tab"}, Scope: command.ScopeGlobal,
			Run: func(command.Context) tea.Cmd { return msgCmd(switchPaneMsg{}) }},
		{ID: "help", Title: "help", Keys: []string{"?"}, Scope: command.ScopeGlobal,
			Run: func(ctx command.Context) tea.Cmd {
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewHelp(m.commands(), ctx, m.d.Theme)})
			}},
		{ID: "trigger", Title: "trigger", Keys: []string{"r"}, Scope: command.ScopeGlobal,
			Enabled: func(ctx command.Context) bool { return ctx.Resource != nil },
			Run:     func(ctx command.Context) tea.Cmd { return m.trigger(ctx.Resource.Name()) }},
		{ID: "open-logs", Title: "open logs", Keys: []string{"enter"}, Scope: command.ScopeList,
			Enabled: func(ctx command.Context) bool { return ctx.Narrow && !m.list.OnHeader() },
			Run:     func(command.Context) tea.Cmd { return msgCmd(showPaneMsg{focus: command.FocusLogs}) }},
		{ID: "back", Title: "back to list", Keys: []string{"esc"}, Scope: command.ScopeLogs,
			Enabled: func(ctx command.Context) bool { return ctx.Narrow },
			Run:     func(command.Context) tea.Cmd { return msgCmd(showPaneMsg{focus: command.FocusList}) }},

		{ID: "list-move", Title: "move", Keys: []string{"j", "k"}, Scope: command.ScopeList},
		{ID: "list-ends", Title: "first/last", Keys: []string{"g", "G"}, Scope: command.ScopeList},
		{ID: "list-expand", Title: "expand/collapse all", Keys: []string{"e", "c"}, Scope: command.ScopeList},
		{ID: "list-disabled", Title: "show/hide disabled", Keys: []string{"d"}, Scope: command.ScopeList},

		{ID: "logs-scroll", Title: "scroll", Keys: []string{"j", "k", "pgup", "pgdown"}, Scope: command.ScopeLogs},
		{ID: "logs-ends", Title: "top/bottom", Keys: []string{"g", "G"}, Scope: command.ScopeLogs},
		{ID: "logs-follow", Title: "follow on/off", Keys: []string{"s"}, Scope: command.ScopeLogs},
		{ID: "logs-timestamps", Title: "timestamps on/off", Keys: []string{"t"}, Scope: command.ScopeLogs},
	}
	for _, f := range features {
		if f.Commands != nil {
			cmds = append(cmds, f.Commands(m)...)
		}
	}
	return cmds
}

func (m *Model) context() command.Context {
	return command.Context{
		Focus:    m.focus,
		Resource: m.list.SelectedResource(),
		Narrow:   m.narrow(),
	}
}

// routeKey sends k to the top overlay if there is one. Otherwise it runs
// the first enabled command bound to k in the focused pane's scope, or
// passes k to the focused pane.
func (m Model) routeKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	ks := k.String()
	if ks == "ctrl+c" {
		return m, tea.Quit
	}
	if n := len(m.overlays); n > 0 {
		top, cmd := m.overlays[n-1].Update(k)
		m.overlays = append(slices.Clone(m.overlays[:n-1]), top)
		return m, cmd
	}
	ctx := m.context()
	for _, c := range m.commands() {
		if !slices.Contains(c.Keys, ks) || !inScope(c.Scope, ctx.Focus) {
			continue
		}
		if c.Enabled != nil && !c.Enabled(ctx) {
			continue
		}
		if c.Run != nil {
			return m, c.Run(ctx)
		}
		break
	}
	if m.focus == command.FocusLogs {
		m.logs.HandleKey(k)
	} else {
		m.list.HandleKey(k)
	}
	return m, nil
}

func inScope(s command.Scope, f command.Focus) bool {
	switch s {
	case command.ScopeGlobal:
		return true
	case command.ScopeList:
		return f == command.FocusList
	case command.ScopeLogs:
		return f == command.FocusLogs
	}
	return false
}

// trigger flashes the start, runs Client.Trigger, then flashes the result.
func (m *Model) trigger(name string) tea.Cmd {
	client, now := m.d.Client, m.d.Now
	return tea.Batch(
		msgCmd(flashMsg{text: "triggering " + name + "…", at: now()}),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), triggerTimeout)
			defer cancel()
			text := "triggered " + name
			if err := client.Trigger(ctx, name); err != nil {
				text = "trigger failed: " + err.Error()
			}
			return flashMsg{text: text, at: now()}
		},
	)
}

func msgCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
