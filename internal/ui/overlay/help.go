package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// helpGroups orders the help sections and names them.
var helpGroups = []struct {
	scope command.Scope
	title string
}{
	{command.ScopeGlobal, "Global"},
	{command.ScopeList, "Resource list"},
	{command.ScopeLogs, "Logs"},
}

// help lists the commands enabled in the context it was opened in. It is a
// pointer so View can record the height that Update scrolls against.
type help struct {
	lines  []string
	offset int // first line drawn
	lastH  int // height from the last View
}

// NewHelp returns the help overlay for every command in cmds that is
// enabled in ctx, grouped by scope.
func NewHelp(cmds []command.Command, ctx command.Context, th theme.Theme) Overlay {
	enabled := make([]command.Command, 0, len(cmds))
	keysW := 0
	for _, c := range cmds {
		if c.Enabled != nil && !c.Enabled(ctx) {
			continue
		}
		enabled = append(enabled, c)
		keysW = max(keysW, ansi.StringWidth(strings.Join(c.Keys, "/")))
	}

	h := &help{}
	for _, g := range helpGroups {
		var rows []string
		for _, c := range enabled {
			if c.Scope != g.scope {
				continue
			}
			keys := strings.Join(c.Keys, "/")
			pad := strings.Repeat(" ", keysW-ansi.StringWidth(keys))
			rows = append(rows, "  "+keys+pad+"  "+c.Title)
		}
		if len(rows) == 0 {
			continue
		}
		if len(h.lines) > 0 {
			h.lines = append(h.lines, "")
		}
		h.lines = append(h.lines, th.Title.Render(g.title))
		h.lines = append(h.lines, rows...)
	}
	return h
}

func (h *help) Title() string { return "Help" }

func (h *help) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return h, nil
	}
	switch k.String() {
	case "esc", "?":
		return h, func() tea.Msg { return CloseMsg{} }
	case "j":
		h.offset = min(h.offset+1, max(0, len(h.lines)-h.lastH))
	case "k":
		h.offset = max(h.offset-1, 0)
	}
	return h, nil
}

// View returns at most height lines, each at most width cells wide,
// starting at the scroll offset.
func (h *help) View(width, height int) string {
	h.lastH = height
	h.offset = max(0, min(h.offset, len(h.lines)-height))
	end := min(len(h.lines), h.offset+height)
	out := make([]string, 0, end-h.offset)
	for _, l := range h.lines[h.offset:end] {
		out = append(out, ansi.Truncate(l, width, "…"))
	}
	return strings.Join(out, "\n")
}
