package overlay

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/fuzzy"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/resourcelist"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// pickerWidth is the widest the picker grows.
const pickerWidth = 48

// ungroupedName matches the resource list's group for unlabelled
// resources. It sorts last.
const ungroupedName = "ungrouped"

// PickMsg names the resource the user picked. Name is the raw Tilt name,
// so it matches the store and resourcelist.Select.
type PickMsg struct{ Name string }

type pickItem struct {
	name   string // raw, for PickMsg
	shown  string // termsafe.Text(name), for matching and drawing
	group  string // termsafe.Text of the group key
	status tilt.Status
}

// pickRow is one line under the field: a group header or an item.
type pickRow struct {
	header string // set on header rows
	item   int    // index into items on item rows
}

// Picker finds a resource by fuzzy name. Results are grouped by the
// resource list's label rule. Groups are ordered by their best match and
// items by rank inside a group.
type Picker struct {
	th     theme.Theme
	field  *TextField
	items  []pickItem
	rows   []pickRow
	hits   []int // item indexes in display order
	cursor int   // index into hits
	offset int   // first result row drawn
}

// NewPicker returns a focused picker over rs, in store order.
func NewPicker(th theme.Theme, rs []tilt.Resource) *Picker {
	p := &Picker{th: th, field: NewTextField(th, "› ", "resource name")}
	p.field.Focus()
	for _, r := range rs {
		p.items = append(p.items, pickItem{
			name:   r.Name(),
			shown:  termsafe.Text(r.Name()),
			group:  termsafe.Text(resourcelist.GroupKey(r.Metadata.Labels)),
			status: r.Combined(),
		})
	}
	p.refresh()
	return p
}

func (p *Picker) Title() string { return "Go to resource" }

// Value returns the query as typed so far.
func (p *Picker) Value() string { return p.field.Value() }

// Selected returns the raw name under the cursor. ok is false when
// nothing matches.
func (p *Picker) Selected() (name string, ok bool) {
	if len(p.hits) == 0 {
		return "", false
	}
	return p.items[p.hits[p.cursor]].name, true
}

// refresh ranks the items against the query and rebuilds the rows. The
// cursor goes back to the best match.
func (p *Picker) refresh() {
	shown := make([]string, len(p.items))
	for i, it := range p.items {
		shown[i] = it.shown
	}
	ranked := fuzzy.Rank(p.field.Value(), shown)
	var order []string
	byGroup := map[string][]int{}
	for _, m := range ranked {
		g := p.items[m.Index].group
		if _, ok := byGroup[g]; !ok {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], m.Index)
	}
	if p.field.Value() == "" {
		// With no query, groups sort as the resource list sorts them.
		sort.SliceStable(order, func(a, b int) bool {
			if (order[a] == ungroupedName) != (order[b] == ungroupedName) {
				return order[b] == ungroupedName
			}
			return order[a] < order[b]
		})
	}
	headers := len(order) > 1 || (len(order) == 1 && order[0] != ungroupedName)
	p.rows, p.hits = p.rows[:0], p.hits[:0]
	for _, g := range order {
		if headers {
			p.rows = append(p.rows, pickRow{header: g})
		}
		for _, i := range byGroup[g] {
			p.rows = append(p.rows, pickRow{item: i})
			p.hits = append(p.hits, i)
		}
	}
	p.cursor, p.offset = 0, 0
}

// Update moves the cursor with up and down, picks with enter and closes
// with esc. Every other key and every paste edits the query.
func (p *Picker) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return p, func() tea.Msg { return CloseMsg{} }
		case "enter":
			name, ok := p.Selected()
			if !ok {
				return p, nil
			}
			return p, tea.Batch(
				func() tea.Msg { return PickMsg{Name: name} },
				func() tea.Msg { return CloseMsg{} },
			)
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

// View draws the field, then the grouped results in the rows left. It
// scrolls so the cursor stays in view.
func (p *Picker) View(width, height int) string {
	w := min(width, pickerWidth)
	lines := []string{p.field.View(w)}
	room := height - 1
	if room <= 0 {
		return lines[0]
	}
	if len(p.rows) == 0 {
		return strings.Join(append(lines, p.th.Dim.Render("no match")), "\n")
	}
	cur := p.cursorRow()
	if cur < p.offset {
		p.offset = cur
	}
	if cur >= p.offset+room {
		p.offset = cur - room + 1
	}
	p.offset = max(0, min(p.offset, len(p.rows)-room))
	end := min(len(p.rows), p.offset+room)
	for ri := p.offset; ri < end; ri++ {
		lines = append(lines, p.renderRow(ri, ri == cur, w))
	}
	return strings.Join(lines, "\n")
}

// cursorRow is the row index of the item under the cursor.
func (p *Picker) cursorRow() int {
	n := -1
	for ri, r := range p.rows {
		if r.header == "" {
			n++
			if n == p.cursor {
				return ri
			}
		}
	}
	return 0
}

func (p *Picker) renderRow(ri int, selected bool, w int) string {
	r := p.rows[ri]
	if r.header != "" {
		return p.th.Title.Render(ansi.Truncate(r.header, w, "…"))
	}
	it := p.items[r.item]
	name := ansi.Truncate(it.shown, max(0, w-4), "…")
	if selected {
		text := "  ● " + name
		text += strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
		return p.th.Selected.Render(text)
	}
	return "  " + p.th.Status[it.status].Render("●") + " " + name
}
