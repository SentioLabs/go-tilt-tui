package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
)

// Messages main sends under --up.
type (
	// ChildStartedMsg says main started `tilt up`. The status line shows
	// "starting tilt…" until the first complete snapshot or the child's
	// exit.
	ChildStartedMsg struct{}
	// ChildExitedMsg says the `tilt up` child ended. Code is its exit
	// code, or -1 when it has none (killed by a signal). Stderr is the
	// tail of its stderr.
	ChildExitedMsg struct {
		Code   int
		Stderr string
	}
)

// childTailLines caps how many stderr lines the panel keeps.
const childTailLines = 500

// childState is what the child feature knows about the --up child. It
// lives in the Model through featureState, so each Model has its own.
type childState struct {
	starting  bool
	exited    bool
	code      int
	lines     []string // the stderr tail, each line through termsafe.Text
	hidePanel bool     // x shows the logs instead of the panel
}

func init() { register(childFeature()) }

// childStateOf returns m's child state.
func childStateOf(m *Model) *childState {
	return featureState(m, "child", func() *childState { return &childState{} })
}

// childFeature shows the state of the `tilt up` child that main runs
// under --up. Without --up no ChildStartedMsg arrives and it shows
// nothing.
func childFeature() Feature {
	return Feature{
		Name: "child",
		Commands: func(m *Model) []command.Command {
			st := childStateOf(m)
			if !st.exited {
				return nil
			}
			return []command.Command{{
				ID: "child-stderr", Title: "show tilt stderr / logs", Keys: []string{"x"}, Scope: command.ScopeGlobal,
				Run: func(command.Context) tea.Cmd {
					st.hidePanel = !st.hidePanel
					return nil
				},
			}}
		},
		// Update never reports handled, so the root model and later
		// features see every message too.
		Update: func(m *Model, msg tea.Msg) (tea.Cmd, bool) {
			st := childStateOf(m)
			switch msg := msg.(type) {
			case ChildStartedMsg:
				*st = childState{starting: true}
			case ChildExitedMsg:
				*st = childState{exited: true, code: msg.Code, lines: stderrLines(msg.Stderr, childTailLines)}
			case ViewMsg:
				if msg.IsComplete {
					st.starting = false
				}
			}
			return nil, false
		},
		ConnStatus: func(m *Model) (string, bool) {
			st, th := childStateOf(m), m.d.Theme
			switch {
			case st.exited:
				return th.Status[tilt.StatusUnhealthy].Render("● " + exitText(st.code)), true
			case st.starting:
				return th.Status[tilt.StatusPending].Render("● starting tilt…"), true
			}
			return "", false
		},
		Hints: []Hint{{ID: "child-stderr", Label: "stderr"}},
		Panel: func(m *Model) (string, string, bool) {
			st := childStateOf(m)
			if !st.exited || st.hidePanel {
				return "", "", false
			}
			title := "tilt stderr · x shows logs"
			if len(st.lines) == 0 {
				return title, "tilt wrote nothing to stderr", true
			}
			// The pane is the screen less the footer row and two border
			// rows. Show the lines that fit, ending with the last.
			rows := max(1, m.height-3)
			return title, strings.Join(st.lines[max(0, len(st.lines)-rows):], "\n"), true
		},
	}
}

// exitText is the header text for a child that ended with code.
func exitText(code int) string {
	if code < 0 {
		return "tilt exited"
	}
	return fmt.Sprintf("tilt exited (code %d)", code)
}

// stderrLines splits s into lines, drops trailing blank lines and keeps
// the last n. Each line passes through termsafe.Text, because tilt's
// stderr may carry any escape sequence.
func stderrLines(s string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = lines[max(0, len(lines)-n):]
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = termsafe.Text(l)
	}
	return out
}
