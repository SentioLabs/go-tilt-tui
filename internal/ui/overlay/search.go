package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// searchWidth is the widest the search field grows.
const searchWidth = 48

// SearchMsg carries the query the user submitted. An empty Query ends the
// search.
type SearchMsg struct{ Query string }

// Search asks for a log search query. enter sends SearchMsg and closes
// the overlay. esc closes it and sends nothing.
type Search struct {
	field *TextField
	th    theme.Theme
}

// NewSearch returns a focused search overlay holding query, so the last
// search can be edited.
func NewSearch(th theme.Theme, query string) *Search {
	f := NewTextField(th, "/", "search logs")
	f.SetValue(query)
	f.Focus()
	return &Search{field: f, th: th}
}

func (s *Search) Title() string { return "Search logs" }

// Value returns the query as typed so far.
func (s *Search) Value() string { return s.field.Value() }

// Update handles enter and esc. Every other key and every paste goes to
// the field.
func (s *Search) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "enter":
			q := s.field.Value()
			return s, tea.Batch(
				func() tea.Msg { return SearchMsg{Query: q} },
				func() tea.Msg { return CloseMsg{} },
			)
		case "esc":
			return s, func() tea.Msg { return CloseMsg{} }
		}
	}
	return s, s.field.Update(msg)
}

// View draws the field and a key hint in at most width cells.
func (s *Search) View(width, height int) string {
	w := min(width, searchWidth)
	lines := []string{s.field.View(w)}
	if height > 1 {
		lines = append(lines, s.th.Dim.Render(ansi.Truncate("enter search · esc cancel", w, "…")))
	}
	return strings.Join(lines, "\n")
}
