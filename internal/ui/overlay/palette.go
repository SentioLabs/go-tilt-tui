package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/fuzzy"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// paletteWidth is the widest the palette grows.
const paletteWidth = 56

// PaletteRunMsg asks the root model to close the palette and run the
// command with this ID.
type PaletteRunMsg struct{ ID command.ID }

// Palette finds a command by fuzzy title. Enter closes it and runs the
// command under the cursor.
type Palette struct {
	th     theme.Theme
	field  *TextField
	cmds   []command.Command
	hits   []int // cmds indexes in display order
	cursor int   // index into hits
	offset int   // first hit drawn
}

// NewPalette returns a focused palette over cmds. The caller filters
// cmds to the ones that apply now and passes them in list order.
func NewPalette(th theme.Theme, cmds []command.Command) *Palette {
	p := &Palette{th: th, field: NewTextField(th, "› ", "command"), cmds: cmds}
	p.field.Focus()
	p.refresh()
	return p
}

func (p *Palette) Title() string { return "Commands" }

// refresh ranks the titles against the query. The cursor goes back to
// the best match.
func (p *Palette) refresh() {
	titles := make([]string, len(p.cmds))
	for i, c := range p.cmds {
		titles[i] = c.Title
	}
	p.hits = p.hits[:0]
	for _, m := range fuzzy.Rank(p.field.Value(), titles) {
		p.hits = append(p.hits, m.Index)
	}
	p.cursor, p.offset = 0, 0
}

// Update moves the cursor with up and down, runs with enter and closes
// with esc. Every other key and every paste edits the query.
func (p *Palette) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return CloseMsg{} }
		case "enter":
			if len(p.hits) == 0 {
				return p, nil
			}
			id := p.cmds[p.hits[p.cursor]].ID
			return p, func() tea.Msg { return PaletteRunMsg{ID: id} }
		case "up", "ctrl+p":
			p.cursor = max(0, p.cursor-1)
			return p, nil
		case "down", "ctrl+n":
			p.cursor = max(0, min(p.cursor+1, len(p.hits)-1))
			return p, nil
		}
	}
	before := p.field.Value()
	cmd := p.field.Update(msg)
	if p.field.Value() != before {
		p.refresh()
	}
	return p, cmd
}

// View draws the field, then the matching commands in the rows left. It
// scrolls so the cursor stays in view.
func (p *Palette) View(width, height int) string {
	w := min(width, paletteWidth)
	lines := []string{p.field.View(w)}
	room := height - 1
	if room <= 0 {
		return lines[0]
	}
	if len(p.hits) == 0 {
		return strings.Join(append(lines, p.th.Dim.Render("no match")), "\n")
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+room {
		p.offset = p.cursor - room + 1
	}
	p.offset = max(0, min(p.offset, len(p.hits)-room))
	end := min(len(p.hits), p.offset+room)
	for i := p.offset; i < end; i++ {
		lines = append(lines, p.renderRow(p.cmds[p.hits[i]], i == p.cursor, w))
	}
	return strings.Join(lines, "\n")
}

// renderRow draws the title on the left and the keys on the right, in at
// most w cells.
func (p *Palette) renderRow(c command.Command, selected bool, w int) string {
	keys := ansi.Truncate(strings.Join(c.Keys, "/"), max(0, w-4), "…")
	title := ansi.Truncate(c.Title, max(0, w-3-ansi.StringWidth(keys)), "…")
	text := "  " + title
	pad := strings.Repeat(" ", max(1, w-ansi.StringWidth(text)-ansi.StringWidth(keys)))
	if selected {
		return p.th.Selected.Render(ansi.Truncate(text+pad+keys, w, ""))
	}
	return ansi.Truncate(text+pad+p.th.Dim.Render(keys), w, "")
}
