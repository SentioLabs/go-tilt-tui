// Package ui is the Bubble Tea front end. It owns the tilt.Store and only
// mutates it from Update, so no locking is needed.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// Messages sent into the program from the watch goroutine.
type (
	// ViewMsg carries one /ws/view message.
	ViewMsg tilt.View
	// ConnMsg reports a connection state change. Err is nil while
	// (re)connecting and set after a drop.
	ConnMsg struct {
		Connecting bool
		Err        error
	}
)

type triggerDoneMsg struct {
	name string
	err  error
}

// Triggerer is the slice of tilt.Client the UI needs. Tests can fake it.
type Triggerer interface {
	Trigger(ctx context.Context, name string) error
}

type connState int

const (
	connConnecting connState = iota
	connLive
	connDown
)

type Model struct {
	client Triggerer
	addr   string
	store  *tilt.Store

	rows   []tilt.Resource
	cursor int // index into rows
	offset int // first visible row

	conn    connState
	connErr error
	flash   string

	width, height int
}

func New(client Triggerer, addr string) Model {
	return Model{client: client, addr: addr, store: tilt.NewStore()}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()

	case ViewMsg:
		m.conn, m.connErr = connLive, nil
		m.applyView(tilt.View(msg))

	case ConnMsg:
		if msg.Connecting {
			m.conn = connConnecting
		} else {
			m.conn, m.connErr = connDown, msg.Err
		}

	case triggerDoneMsg:
		if msg.err != nil {
			m.flash = "trigger failed: " + msg.err.Error()
		} else {
			m.flash = "triggered " + msg.name
		}

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyView merges an update while keeping the cursor on the same resource
// name, so rows moving around under the user don't change their selection.
func (m *Model) applyView(v tilt.View) {
	selected := m.selectedName()
	m.store.Apply(v)
	m.rows = m.store.Resources()
	m.cursor = 0
	for i, r := range m.rows {
		if r.Name() == selected {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

func (m Model) selectedName() string {
	if m.cursor < len(m.rows) {
		return m.rows[m.cursor].Name()
	}
	return ""
}

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(len(m.rows)-1, 0)
	case "r", "enter":
		if name := m.selectedName(); name != "" {
			m.flash = "triggering " + name + "…"
			return m, m.trigger(name)
		}
	}
	m.clampScroll()
	return m, nil
}

func (m Model) trigger(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return triggerDoneMsg{name: name, err: m.client.Trigger(ctx, name)}
	}
}

// listHeight is the number of rows available for resources.
func (m Model) listHeight() int {
	return max(m.height-3, 1) // header, blank line, footer
}

func (m *Model) clampScroll() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(min(m.offset, len(m.rows)-h), 0)
}

var (
	styleTitle   = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleCursor  = lipgloss.NewStyle().Reverse(true)
	styleErrText = lipgloss.NewStyle().Foreground(lipgloss.Red)

	statusDot = map[tilt.Status]lipgloss.Style{
		tilt.StatusNone:      lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
		tilt.StatusPending:   lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		tilt.StatusBuilding:  lipgloss.NewStyle().Foreground(lipgloss.Blue),
		tilt.StatusHealthy:   lipgloss.NewStyle().Foreground(lipgloss.Green),
		tilt.StatusUnhealthy: lipgloss.NewStyle().Foreground(lipgloss.Red),
		tilt.StatusDisabled:  lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
	}
)

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "tilt-tui " + m.addr
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")

	nameW := 12
	for _, r := range m.rows {
		nameW = max(nameW, ansi.StringWidth(termsafe.Text(r.Name())))
	}
	nameW = min(nameW, m.width/2)

	h := m.listHeight()
	lines := make([]string, 0, h)
	if len(m.rows) == 0 {
		lines = append(lines, styleDim.Render("  no resources yet"))
	}
	for i := m.offset; i < min(m.offset+h, len(m.rows)); i++ {
		r := m.rows[i]
		st := r.Combined()
		line := fmt.Sprintf(" %s %-*s  %-8s  %-8s  %s",
			statusDot[st].Render("●"),
			nameW, ansi.Truncate(termsafe.Text(r.Name()), nameW, "…"),
			termsafe.Text(r.Kind()),
			st,
			termsafe.Text(firstLine(r.LastBuildError())),
		)
		line = ansi.Truncate(line, m.width, "…")
		if i == m.cursor {
			// Pad so the highlight spans the full width.
			line = styleCursor.Render(line + strings.Repeat(" ", max(m.width-ansi.StringWidth(line), 0)))
		}
		lines = append(lines, line)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	var conn string
	switch m.conn {
	case connConnecting:
		conn = statusDot[tilt.StatusPending].Render("● connecting")
	case connLive:
		conn = statusDot[tilt.StatusHealthy].Render("● live")
	case connDown:
		conn = statusDot[tilt.StatusUnhealthy].Render("● disconnected")
		if m.connErr != nil {
			conn += " " + styleErrText.Render(termsafe.Text(m.connErr.Error()))
		}
	}
	h := fmt.Sprintf("%s  %s  %s", styleTitle.Render("tilt-tui"), styleDim.Render(m.addr), conn)
	if tf := m.store.TiltfilePath(); tf != "" {
		h += "  " + styleDim.Render(termsafe.Text(tf))
	}
	return ansi.Truncate(h, m.width, "…")
}

func (m Model) footer() string {
	f := "↑/↓ move  r trigger  q quit"
	if m.flash != "" {
		f = termsafe.Text(m.flash) + "  ·  " + f
	}
	return styleDim.Render(ansi.Truncate(f, m.width, "…"))
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
