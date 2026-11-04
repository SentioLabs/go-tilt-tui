package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// RestartRequestMsg asks main to start `tilt up` again with the same
// arguments. R sends it. main's program filter takes it out of the
// message stream and runs the restart, so Update never sees it.
type RestartRequestMsg struct{}

// restartState tracks the --up child from the messages main sends. It
// lives in the Model through featureState, so each Model has its own.
type restartState struct {
	exited    bool // a ChildExitedMsg arrived and no ChildStartedMsg since
	requested bool // R was pressed and main hasn't answered yet
}

// restartingText is the flash R shows while main restarts tilt.
const restartingText = "restarting tilt…"

func init() { register(restartFeature()) }

// restartStateOf returns m's restart state.
func restartStateOf(m *Model) *restartState {
	return featureState(m, "restart", func() *restartState { return &restartState{} })
}

// restartFeature binds R to restart tilt. R is enabled only under --up
// after the child exited, and only once until main reports a new start
// or another exit.
func restartFeature() Feature {
	return Feature{
		Name: "restart",
		Commands: func(m *Model) []command.Command {
			st := restartStateOf(m)
			if !st.exited || st.requested {
				return nil
			}
			return []command.Command{{
				ID: "restart", Title: "restart tilt", Keys: []string{"R"}, Scope: command.ScopeGlobal,
				Run: func(command.Context) tea.Cmd {
					st.requested = true
					// Sequence delivers the flash to Update before main sees
					// the request, so a fast exit can't beat it.
					return tea.Sequence(m.flashCmd(restartingText, 0), msgCmd(RestartRequestMsg{}))
				},
			}}
		},
		// Update never reports handled: the child feature needs the same
		// messages.
		Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
			st := restartStateOf(m)
			switch msg.(type) {
			case ChildStartedMsg:
				*st = restartState{}
			case ChildExitedMsg:
				*st = restartState{exited: true}
				// A restart that fails fast exits while its flash still shows.
				// Clear it so the footer shows the R hint again. The clear keeps
				// the flash's stamp so a later flash that arrives after this
				// exit still wins.
				if m.flashActive() && m.flash == restartingText {
					m.setFlash("", m.flashAt, 0)
				}
			}
			return nil, false
		},
		Hints: []Hint{{ID: "restart", Label: "restart tilt"}},
	}
}
