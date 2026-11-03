// Package resourcelist is the left pane: an "All Resources" row, then the
// resources grouped by label in collapsible groups.
package resourcelist

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// ungrouped is the group for resources with no non-empty label.
const ungrouped = "ungrouped"

// minTimeWidth is the narrowest pane that still shows relative times.
const minTimeWidth = 30

// groupLabels are checked in order before falling back to any label.
var groupLabels = []string{
	"app",
	"app.kubernetes.io/name",
	"app.kubernetes.io/component",
	"component",
	"service",
	"tilt.dev/resource",
}

// Counts summarises every resource for the header.
type Counts struct {
	Errors, Warnings, Busy, Healthy, Total int
}

type rowKind int

const (
	rowAll rowKind = iota
	rowHeader
	rowResource
)

// row is one line of the list. group is set on headers and resources.
// res indexes Model.resources and is only meaningful on resource rows.
type row struct {
	kind  rowKind
	group string
	res   int
}

// target is what the cursor points at, kept by name so it survives
// SetResources reordering the rows.
type target struct {
	kind rowKind
	name string // group name for a header, resource name for a resource
}

type group struct {
	name    string
	members []int // indexes into Model.resources, in store order
}

type Model struct {
	th        theme.Theme
	resources []tilt.Resource
	groups    []group
	grouped   bool // false when every resource is ungrouped: no headers
	rows      []row

	cursor int
	target target
	source logstore.Source

	collapsed    map[string]bool
	hideDisabled bool
	// pinned names a resource that stays listed even though visible
	// rejects it, because the cursor was on it when it was filtered out.
	pinned string

	offset    int  // first row drawn by View
	showsTime bool // the last View drew a relative time
}

func New(th theme.Theme) *Model {
	m := &Model{
		th:        th,
		target:    target{kind: rowAll},
		source:    logstore.Source{All: true},
		collapsed: map[string]bool{},
	}
	m.rebuild()
	return m
}

// GroupKey returns the group a resource with these labels belongs to.
func GroupKey(labels map[string]string) string {
	for _, k := range groupLabels {
		if v := labels[k]; v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ungrouped
}

func (m *Model) SetResources(rs []tilt.Resource) {
	m.resources = rs
	byName := map[string]int{}
	m.groups = m.groups[:0]
	for i, r := range rs {
		g := GroupKey(r.Metadata.Labels)
		gi, ok := byName[g]
		if !ok {
			gi = len(m.groups)
			byName[g] = gi
			m.groups = append(m.groups, group{name: g})
		}
		m.groups[gi].members = append(m.groups[gi].members, i)
	}
	sort.Slice(m.groups, func(a, b int) bool {
		ga, gb := m.groups[a].name, m.groups[b].name
		if (ga == ungrouped) != (gb == ungrouped) {
			return gb == ungrouped
		}
		return ga < gb
	})
	m.grouped = len(m.groups) > 1 || (len(m.groups) == 1 && m.groups[0].name != ungrouped)
	m.rebuild()
}

// visible is the filter rule. The pinned resource is exempt from it.
func (m *Model) visible(r tilt.Resource) bool {
	return !(m.hideDisabled && r.IsDisabled())
}

// rebuild recomputes the rows and puts the cursor back on its target.
// If the target is gone the cursor keeps its index, clamped to the list.
func (m *Model) rebuild() {
	m.rows = append(m.rows[:0], row{kind: rowAll})
	for _, g := range m.groups {
		if m.grouped {
			m.rows = append(m.rows, row{kind: rowHeader, group: g.name})
			if m.collapsed[g.name] {
				continue
			}
		}
		for _, i := range g.members {
			r := m.resources[i]
			if m.visible(r) || r.Name() == m.pinned {
				m.rows = append(m.rows, row{kind: rowResource, group: g.name, res: i})
			}
		}
	}
	found := false
	for i, r := range m.rows {
		if m.targetOf(r) == m.target {
			m.cursor, found = i, true
			break
		}
	}
	if !found {
		m.cursor = min(m.cursor, len(m.rows)-1)
	}
	m.syncCursor()
	m.dropStalePin()
}

// dropStalePin unpins the pinned resource once the cursor is off it, so
// the filter removes it from the list.
func (m *Model) dropStalePin() {
	if m.pinned != "" && m.target != (target{kind: rowResource, name: m.pinned}) {
		m.pinned = ""
		m.rebuild()
	}
}

func (m *Model) targetOf(r row) target {
	switch r.kind {
	case rowHeader:
		return target{kind: rowHeader, name: r.group}
	case rowResource:
		return target{kind: rowResource, name: m.resources[r.res].Name()}
	default:
		return target{kind: rowAll}
	}
}

// syncCursor records what the cursor is on. A header leaves the source
// alone, so moving through headers never changes the logs.
func (m *Model) syncCursor() {
	r := m.rows[m.cursor]
	m.target = m.targetOf(r)
	switch r.kind {
	case rowAll:
		m.source = logstore.Source{All: true}
	case rowResource:
		m.source = logstore.Source{Resource: m.target.name}
	}
}

// moveTo puts the cursor on row i.
func (m *Model) moveTo(i int) {
	m.cursor = max(0, min(i, len(m.rows)-1))
	m.syncCursor()
	m.dropStalePin()
}

func (m *Model) HandleKey(k tea.KeyPressMsg) (selectionChanged bool) {
	before := m.source
	switch k.String() {
	case "j", "down":
		m.moveTo(m.cursor + 1)
	case "k", "up":
		m.moveTo(m.cursor - 1)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.rows) - 1)
	case "enter":
		if !m.OnHeader() {
			return false
		}
		g := m.rows[m.cursor].group
		m.collapsed[g] = !m.collapsed[g]
		m.rebuild()
	case "e":
		clear(m.collapsed)
		m.rebuild()
	case "c":
		for _, g := range m.groups {
			m.collapsed[g.name] = true
		}
		if r := m.rows[m.cursor]; m.grouped && r.kind == rowResource {
			m.target = target{kind: rowHeader, name: r.group}
		}
		m.rebuild()
	case "d":
		m.hideDisabled = !m.hideDisabled
		m.pinned = ""
		if r := m.rows[m.cursor]; r.kind == rowResource {
			m.pinned = m.target.name
		}
		m.rebuild()
	default:
		return false
	}
	return m.source != before
}

func (m *Model) Selected() logstore.Source { return m.source }

// SelectedResource returns the resource under the cursor, or nil on the
// All row or a header.
func (m *Model) SelectedResource() *tilt.Resource {
	r := m.rows[m.cursor]
	if r.kind != rowResource {
		return nil
	}
	res := m.resources[r.res]
	return &res
}

// OnHeader reports whether the cursor is on a group header.
func (m *Model) OnHeader() bool { return m.rows[m.cursor].kind == rowHeader }

// Counts covers every resource, whatever is collapsed or hidden.
func (m *Model) Counts() Counts {
	var c Counts
	for _, r := range m.resources {
		switch r.Combined() {
		case tilt.StatusUnhealthy:
			c.Errors++
		case tilt.StatusPending, tilt.StatusBuilding:
			c.Busy++
		case tilt.StatusHealthy:
			c.Healthy++
		}
		if r.HasWarning() {
			c.Warnings++
		}
		if !r.IsDisabled() {
			c.Total++
		}
	}
	return c
}

// View returns exactly h lines, each at most w cells wide, scrolled so the
// cursor row is visible.
func (m *Model) View(w, h int, focused bool, now time.Time) string {
	m.showsTime = false
	if h <= 0 {
		return ""
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, min(m.offset, len(m.rows)-h))

	lines := make([]string, h)
	for i := range lines {
		ri := m.offset + i
		if ri >= len(m.rows) {
			break
		}
		lines[i] = m.renderRow(ri, w, focused, now)
	}
	return strings.Join(lines, "\n")
}

// ShowsRelativeTime reports whether the last View drew any relative time.
func (m *Model) ShowsRelativeTime() bool { return m.showsTime }

func (m *Model) renderRow(ri, w int, focused bool, now time.Time) string {
	r := m.rows[ri]
	selected := ri == m.cursor
	var text, styled string
	switch r.kind {
	case rowAll:
		text = ansi.Truncate("All Resources", w, "…")
		styled = text
	case rowHeader:
		text = ansi.Truncate(m.headerText(r.group), w, "…")
		styled = text
	case rowResource:
		text, styled = m.resourceRow(m.resources[r.res], w, now)
	}
	if !selected {
		return styled
	}
	if focused {
		text += strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
		return m.th.Selected.Render(text)
	}
	return m.th.Selected.Faint(true).Render(text)
}

func (m *Model) headerText(name string) string {
	arrow := "▼"
	if m.collapsed[name] {
		arrow = "▶"
	}
	n := 0
	for _, g := range m.groups {
		if g.name == name {
			n = len(g.members)
		}
	}
	return fmt.Sprintf("%s %s (%d)", arrow, termsafe.Text(name), n)
}

// resourceRow lays out "<dot><warn> <name>", padding, then the relative
// time. It returns the row as plain text and with the theme applied.
func (m *Model) resourceRow(r tilt.Resource, w int, now time.Time) (text, styled string) {
	indent := ""
	if m.grouped {
		indent = "  "
	}
	warn := " "
	if r.HasWarning() {
		warn = "!"
	}
	name := termsafe.Text(r.Name())
	if !m.visible(r) {
		name += " (filtered)"
	}
	right := ""
	if w >= minTimeWidth {
		right = relativeTime(r, now)
	}

	prefixW := len(indent) + 3 // dot, warn, space
	nameW := w - prefixW
	if right != "" {
		nameW -= ansi.StringWidth(right) + 1
	}
	name = ansi.Truncate(name, max(0, nameW), "…")
	pad := ""
	if right != "" {
		pad = strings.Repeat(" ", max(1, w-prefixW-ansi.StringWidth(name)-ansi.StringWidth(right)))
		m.showsTime = true
	}

	text = ansi.Truncate(indent+"●"+warn+" "+name+pad+right, w, "")
	if r.IsDisabled() || !m.visible(r) {
		return text, m.th.Dim.Render(text)
	}
	if r.HasWarning() {
		warn = m.th.Warning.Render(warn)
	}
	dot := m.th.Status[r.Combined()].Render("●")
	styled = ansi.Truncate(indent+dot+warn+" "+name+pad+right, w, "")
	return text, styled
}

// relativeTime is "<ago> · <duration>" for the latest build, or "" when
// it hasn't finished.
func relativeTime(r tilt.Resource, now time.Time) string {
	if len(r.Status.BuildHistory) == 0 {
		return ""
	}
	b := r.Status.BuildHistory[0]
	if b.FinishTime == nil {
		return ""
	}
	s := ago(now.Sub(*b.FinishTime))
	if b.StartTime != nil {
		s += " · " + b.FinishTime.Sub(*b.StartTime).Round(100*time.Millisecond).String()
	}
	return s
}

func ago(d time.Duration) string {
	d = max(0, d)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}
