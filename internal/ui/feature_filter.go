package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/resourcelist"
)

// filterFeature is the status filter: f cycles all, errors, in progress
// and healthy, and esc in the list clears it. The filter itself lives in
// the resource list, so this feature keeps no state.
var filterFeature = Feature{
	Name:      "filter",
	Commands:  filterCommands,
	ListTitle: filterTitle,
	Hints:     []Hint{{ID: "filter", Label: "filter"}},
}

func init() { register(filterFeature) }

func filterCommands(m *Model) []command.Command {
	return []command.Command{
		{ID: "filter", Title: "status filter", Keys: []string{"f"}, Scope: command.ScopeList,
			Run: func(command.Context) tea.Cmd {
				m.list.SetFilter(m.list.Filter().Next())
				return nil
			}},
		{ID: "filter-clear", Title: "clear status filter", Keys: []string{"esc"}, Scope: command.ScopeList,
			Enabled: func(command.Context) bool { return m.list.Filter() != resourcelist.FilterAll },
			Run: func(command.Context) tea.Cmd {
				m.list.SetFilter(resourcelist.FilterAll)
				return nil
			}},
	}
}

// filterTitle names the active filter after the header counts, as
// "[errors]". It stays short because the list pane is narrow. The
// counts themselves always cover every resource.
func filterTitle(m *Model) string {
	f := m.list.Filter()
	if f == resourcelist.FilterAll {
		return ""
	}
	return m.d.Theme.Warning.Render("[" + f.String() + "]")
}
