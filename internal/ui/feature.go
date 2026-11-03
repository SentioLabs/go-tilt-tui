package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// Feature adds commands, message handling and screen text to the root
// model. P2 and P3 files in package ui call register from init(), so they
// never edit model.go, commands.go or layout.go.
//
// Every string a hook returns passes through termsafe.LogLine before it
// is drawn. Hooks may style text with theme colours, and no other escape
// sequence reaches the terminal. Every hook is optional.
type Feature struct {
	Name     string
	Commands func(m *Model) []command.Command
	Update   func(m *Model, msg tea.Msg) (cmd tea.Cmd, handled bool)

	// ConnStatus replaces the connection state at the start of the status
	// line ("● live", "● disconnected: …") while ok is true. The first
	// feature that returns ok wins.
	ConnStatus func(m *Model) (text string, ok bool)
	// StatusParts adds parts after the address and uptime in the status
	// line. Empty strings are skipped.
	StatusParts func(m *Model) []string
	// ListTitle adds text after the counts in the list pane title.
	ListTitle func(m *Model) string
	// LogTitle adds text to the log pane title, after "[follow]" and
	// before the saved filter names.
	LogTitle func(m *Model) string
	// Hints adds footer hints after "r trigger" and before "tab switch".
	Hints []Hint
	// Panel replaces the log pane's title and body while ok is true. The
	// first feature that returns ok wins.
	Panel func(m *Model) (title, body string, ok bool)
}

// Hint advertises one command in the footer as "<first key> <Label>". A
// hint whose command isn't in the command list shows nothing. Feature
// hints that don't fit the footer are dropped from the last one back.
type Hint struct {
	ID    command.ID
	Label string
}

var features []Feature

func register(f Feature) { features = append(features, f) }

// PushOverlayMsg asks the root model to push an overlay on the stack.
type PushOverlayMsg struct{ Overlay overlay.Overlay }

// flashCmd returns a command that shows text in the footer for d. A d of
// zero means the default 3 seconds. The flash is stamped now, so a later
// flash replaces it. A tea.Cmd that runs slow work should instead return
// flashMsg{text: …, at: now(), d: …} itself, with now captured from
// m.d.Now before the command starts.
func (m *Model) flashCmd(text string, d time.Duration) tea.Cmd {
	return msgCmd(flashMsg{text: text, at: m.d.Now(), d: d})
}

// featureState returns the state a feature keeps on this Model, creating
// it with init on first use. name is the feature's Name.
func featureState[T any](m *Model, name string, init func() *T) *T {
	if v, ok := m.fstate[name]; ok {
		return v.(*T)
	}
	v := init()
	m.fstate[name] = v
	return v
}
