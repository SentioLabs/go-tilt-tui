package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/logview"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// searchStepMsg moves to the next match, or the previous one when back
// is set.
type searchStepMsg struct{ back bool }

// searchFeature is log search: "/" asks for a query, n and N move between
// matching lines and esc in the log pane ends the search.
var searchFeature = Feature{
	Name:     "search",
	Commands: searchCommands,
	Update:   searchUpdate,
	LogTitle: searchTitle,
	Hints:    []Hint{{ID: "search", Label: "search"}},
}

func init() { register(searchFeature) }

func searchCommands(m *Model) []command.Command {
	active := func(command.Context) bool { return m.logs.Query() != "" }
	return []command.Command{
		{ID: "search", Title: "search logs", Keys: []string{"/"}, Scope: command.ScopeGlobal,
			Run: func(command.Context) tea.Cmd {
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewSearch(m.d.Theme, m.logs.Query())})
			}},
		{ID: "search-next", Title: "next match", Keys: []string{"n"}, Scope: command.ScopeLogs, Enabled: active,
			Run: func(command.Context) tea.Cmd { return msgCmd(searchStepMsg{}) }},
		{ID: "search-prev", Title: "previous match", Keys: []string{"N"}, Scope: command.ScopeLogs, Enabled: active,
			Run: func(command.Context) tea.Cmd { return msgCmd(searchStepMsg{back: true}) }},
		// searchUpdate handles esc before the root model. This entry
		// lists the key in help while a search is active.
		{ID: "search-clear", Title: "clear search", Keys: []string{"esc"}, Scope: command.ScopeLogs, Enabled: active},
	}
}

func searchUpdate(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case overlay.SearchMsg:
		m.logs.SetQuery(msg.Query)
		if msg.Query == "" {
			return nil, true
		}
		m.focus = command.FocusLogs
		searchJump(m, false)
		return nil, true
	case searchStepMsg:
		searchJump(m, msg.back)
		return nil, true
	case tea.PasteMsg:
		// The root model routes only key presses to overlays.
		n := len(m.overlays)
		if n == 0 {
			return nil, false
		}
		if _, ok := m.overlays[n-1].(*overlay.Search); !ok {
			return nil, false
		}
		top, cmd := m.overlays[n-1].Update(msg)
		m.overlays = append(slices.Clone(m.overlays[:n-1]), top)
		return cmd, true
	case tea.KeyPressMsg:
		// esc ends the search before the narrow layout's "back to list"
		// sees it. A second esc then goes back.
		if msg.String() != "esc" || len(m.overlays) > 0 || m.focus != command.FocusLogs || m.logs.Query() == "" {
			return nil, false
		}
		m.logs.SetQuery("")
		return nil, true
	}
	return nil, false
}

// searchJump moves the log view to the next or previous match. It
// flashes when nothing matches.
func searchJump(m *Model, back bool) {
	step := m.logs.NextMatch
	if back {
		step = m.logs.PrevMatch
	}
	if !step(searchParts(m)) {
		m.setFlash("no match for "+termsafe.Text(m.logs.Query()), m.d.Now(), flashFor)
	}
}

// searchParts lists the buffers behind the log view's source. The All
// source covers the global logs and every resource in the store.
func searchParts(m *Model) []logstore.Source {
	rs := m.store.Resources()
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name()
	}
	return logview.Parts(m.logs.Source(), names)
}

// searchTitle shows the query and the match position, like "/err 3/17".
func searchTitle(m *Model) string {
	q := m.logs.Query()
	if q == "" {
		return ""
	}
	s := m.d.Theme.Match.Render("/" + termsafe.Text(q))
	if pos, total, ok := m.logs.MatchPos(); ok {
		s += fmt.Sprintf(" %d/%d", pos, total)
	}
	return s
}
