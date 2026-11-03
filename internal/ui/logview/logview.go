// Package logview is the right pane. It draws only the visible rows of the
// selected log source and keeps follow mode and the scroll position.
package logview

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// maxCacheEntries is the wrap cache size that triggers a full clear.
const maxCacheEntries = 20000

// wheelRows is how many rows one mouse wheel step scrolls.
const wheelRows = 3

const sgrReset = "\x1b[0m"

type cacheKey struct {
	seq     logstore.Seq
	version uint32
	width   int
}

// row is one wrapped row and its width in cells.
type row struct {
	text  string
	width int
}

type Model struct {
	store      *logstore.Store
	th         theme.Theme
	src        logstore.Source
	title      string
	follow     bool
	top        logstore.Seq // first visible line; 0 means the oldest line
	topRow     int          // rows of that line scrolled past
	timestamps bool
	resets     uint64
	cache      map[cacheKey][]row
	lastW      int // text-area width from the last View
	lastH      int

	track, thumb string // scrollbar cells, rendered once

	// Search state. match is the Seq of the current match, 0 for none.
	query      string
	mt         matcher
	matchOn    string // SGR that starts a highlight, from theme.Match
	match      logstore.Seq
	matchPos   int
	matchTotal int
}

func New(store *logstore.Store, th theme.Theme) *Model {
	return &Model{
		store:   store,
		th:      th,
		follow:  true,
		resets:  store.Resets(),
		cache:   map[cacheKey][]row{},
		track:   th.Dim.Render("│"),
		thumb:   "┃",
		matchOn: sgrOpen(th.Match.Render),
	}
}

// SetSource switches the pane to src and turns follow mode on.
func (m *Model) SetSource(src logstore.Source, title string) {
	m.src, m.title = src, title
	m.follow = true
	m.top, m.topRow = 0, 0
	m.match, m.matchPos, m.matchTotal = 0, 0, 0
}

func (m *Model) Source() logstore.Source { return m.src }

func (m *Model) Title() string { return m.title }

func (m *Model) Following() bool { return m.follow }

func (m *Model) Timestamps() bool { return m.timestamps }

// HandleKey reports whether the key belongs to the log view.
func (m *Model) HandleKey(k tea.KeyPressMsg) (handled bool) {
	page := max(1, m.lastH-1)
	switch k.String() {
	case "j", "down":
		m.down(1)
	case "k", "up":
		m.up(1)
	case "pgdown":
		m.down(page)
	case "pgup":
		m.up(page)
	case "g", "home":
		m.follow = false
		m.top, m.topRow = 0, 0
	case "G", "end":
		m.follow = true
	case "s":
		m.follow = !m.follow
	case "t":
		m.timestamps = !m.timestamps
		clear(m.cache)
	default:
		return false
	}
	return true
}

func (m *Model) HandleMouse(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.up(wheelRows)
	case tea.MouseWheelDown:
		m.down(wheelRows)
	}
}

// up scrolls n rows towards older lines and turns follow off. It starts
// from the anchor the last View recorded.
func (m *Model) up(n int) {
	m.follow = false
	m.normalize()
	for range n {
		if m.topRow > 0 {
			m.topRow--
			continue
		}
		prev := m.store.Prev(m.src, m.top, 1)
		if len(prev) == 0 {
			return
		}
		m.top, m.topRow = prev[0].Seq, len(m.rowsOf(prev[0]))-1
	}
}

// down scrolls n rows towards newer lines. It stops once the newest line's
// last row is at the bottom of the pane and never turns follow on.
func (m *Model) down(n int) {
	if m.follow {
		return
	}
	m.normalize()
	for range n {
		lines := m.fromTop(m.lastH + 1)
		if len(lines) == 0 {
			return
		}
		below := -m.topRow
		for _, l := range lines {
			below += len(m.rowsOf(l))
		}
		if below <= m.lastH {
			return
		}
		switch {
		case m.topRow+1 < len(m.rowsOf(lines[0])):
			m.topRow++
		case len(lines) > 1:
			m.top, m.topRow = lines[1].Seq, 0
		default:
			return
		}
	}
}

// fromTop returns up to n lines starting at the anchor line.
func (m *Model) fromTop(n int) []logstore.Line {
	if m.top == 0 {
		return m.store.Next(m.src, 0, n)
	}
	return m.store.Next(m.src, m.top-1, n)
}

// normalize moves an evicted anchor to the next line and clamps topRow to
// the anchor line's rows at the current width.
func (m *Model) normalize() {
	lines := m.fromTop(1)
	if len(lines) == 0 {
		m.topRow = 0
		return
	}
	if lines[0].Seq != m.top {
		m.top, m.topRow = lines[0].Seq, 0
	}
	m.topRow = max(0, min(m.topRow, len(m.rowsOf(lines[0]))-1))
}

// rowsOf returns the cached wrap of l at the last text-area width.
func (m *Model) rowsOf(l logstore.Line) []row {
	k := cacheKey{seq: l.Seq, version: l.Version, width: m.lastW}
	if rs, ok := m.cache[k]; ok {
		return rs
	}
	if len(m.cache) >= maxCacheEntries {
		clear(m.cache)
	}
	text := l.Text
	if m.query != "" && m.mt.match(text) {
		text = highlight(text, m.mt.spans(text), m.matchOn)
	}
	if m.timestamps {
		text = m.th.Dim.Render(l.Time.Local().Format("15:04:05")) + " " + text
	}
	wrapped := wrap(text, m.lastW)
	rs := make([]row, len(wrapped))
	for i, t := range wrapped {
		rs[i] = row{text: t, width: ansi.StringWidth(t)}
	}
	m.cache[k] = rs
	return rs
}

// View returns exactly h lines, each at most w cells wide. The rightmost
// column is the scrollbar. The pane border is drawn by the root model, so
// focused doesn't change the content.
func (m *Model) View(w, h int, focused bool) string {
	if h <= 0 {
		return ""
	}
	m.checkReset()
	m.lastW, m.lastH = max(0, w-1), h

	var visible []row
	var lines int
	if m.follow {
		visible, lines = m.followRows(h)
	} else {
		visible, lines = m.anchoredRows(h)
	}

	var b strings.Builder
	b.Grow(h * (m.lastW + 16))
	if lines == 0 {
		visible = []row{{text: m.th.Dim.Render("no logs yet"), width: len("no logs yet")}}
	}
	bar := m.scrollbar(h, lines)
	for i := range h {
		if i > 0 {
			b.WriteByte('\n')
		}
		if w <= 0 {
			continue
		}
		var r row
		if i < len(visible) {
			r = visible[i]
		}
		if r.width > m.lastW {
			r.text = ansi.Truncate(r.text, m.lastW, "")
			r.width = ansi.StringWidth(r.text)
		}
		b.WriteString(r.text)
		b.WriteString(sgrReset)
		for range m.lastW - r.width {
			b.WriteByte(' ')
		}
		b.WriteString(bar[i])
	}
	return b.String()
}

// checkReset drops every Seq the view holds once the store has reset,
// because a reset restarts Seq. The view then follows the newest lines.
func (m *Model) checkReset() {
	if r := m.store.Resets(); r != m.resets {
		m.resets = r
		clear(m.cache)
		m.follow = true
		m.top, m.topRow = 0, 0
		m.match, m.matchPos, m.matchTotal = 0, 0, 0
	}
}

// followRows returns the last h rows of the newest lines and records the
// line and row offset of the first one as the anchor. lines is how many
// lines the rows come from.
func (m *Model) followRows(h int) (rows []row, lines int) {
	newest := m.store.Prev(m.src, logstore.End, h)
	total := 0
	for _, l := range newest {
		total += len(m.rowsOf(l))
	}
	skip := max(0, total-h)
	rows = make([]row, 0, h)
	for _, l := range newest {
		rs := m.rowsOf(l)
		if skip >= len(rs) {
			skip -= len(rs)
			continue
		}
		if len(rows) == 0 {
			m.top, m.topRow = l.Seq, skip
		}
		rows = append(rows, rs[skip:]...)
		skip = 0
		lines++
	}
	return rows, lines
}

// anchoredRows returns h rows starting at the anchor.
func (m *Model) anchoredRows(h int) (rows []row, lines int) {
	m.normalize()
	skip := m.topRow
	rows = make([]row, 0, h)
	for _, l := range m.fromTop(h) {
		if len(rows) == h {
			break
		}
		rs := m.rowsOf(l)[skip:]
		skip = 0
		rows = append(rows, rs[:min(len(rs), h-len(rows))]...)
		lines++
	}
	return rows, lines
}

// scrollbar returns one cell per row. With no lines it is all track.
func (m *Model) scrollbar(h, visible int) []string {
	bar := make([]string, h)
	for i := range bar {
		bar[i] = m.track
	}
	if visible == 0 {
		return bar
	}
	pos, total := m.store.Rank(m.src, m.top)
	if total == 0 {
		return bar
	}
	size := max(1, h*visible/total)
	start := max(0, min(h*pos/total, h-size))
	for i := start; i < start+size && i < h; i++ {
		bar[i] = m.thumb
	}
	return bar
}
