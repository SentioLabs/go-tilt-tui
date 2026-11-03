package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

const (
	// narrowWidth is the narrowest screen that shows both panes.
	narrowWidth = 100
	// The list pane takes listShare percent of the width, within
	// listMinWidth..listMaxWidth columns.
	listShare    = 30
	listMinWidth = 24
	listMaxWidth = 48

	sgrReset = "\x1b[0m"
)

// footerHints names the commands the footer always advertises, with a
// short label. Feature hints go after the first one.
var footerHints = []Hint{
	{"trigger", "trigger"},
	{"switch-pane", "switch"},
	{"help", "help"},
	{"quit", "quit"},
}

func (m *Model) narrow() bool { return m.width < narrowWidth }

func listWidth(total int) int {
	return min(max(total*listShare/100, listMinWidth), listMaxWidth)
}

// render returns exactly m.height lines: the panes, any overlays drawn
// over them, then the footer.
func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	now := m.d.Now()
	paneH := m.height - 1
	var lines []string
	if paneH > 0 {
		switch {
		case !m.narrow():
			lw := listWidth(m.width)
			left := m.listPane(lw, paneH, m.focus == command.FocusList, now)
			right := m.logPane(m.width-lw, paneH, m.focus == command.FocusLogs)
			lines = make([]string, paneH)
			for i := range lines {
				lines[i] = left[i] + right[i]
			}
		case m.focus == command.FocusLogs:
			lines = m.logPane(m.width, paneH, true)
		default:
			lines = m.listPane(m.width, paneH, true, now)
		}
		for _, o := range m.overlays {
			m.drawOverlay(lines, o)
		}
	}
	return strings.Join(append(lines, m.footer(now)), "\n")
}

func (m *Model) border(focused bool) lipgloss.Style {
	if focused {
		return m.d.Theme.BorderFocused
	}
	return m.d.Theme.Border
}

// listPane is the boxed resource list with the status line in its last
// rows. The status line wraps rather than hiding the remote marker. When
// the pane is too short for every status row, the last row, which holds
// the marker, stays.
func (m *Model) listPane(w, h int, focused bool, now time.Time) []string {
	innerW, innerH := max(0, w-2), max(0, h-2)
	parts := m.statusParts(now, innerW)
	if m.d.Remote {
		parts = append(parts, m.remoteMarker())
	}
	status := wrapParts(parts, innerW)
	if n := len(status); n > innerH {
		last := status[n-1]
		status = status[:innerH]
		if innerH > 0 {
			status[innerH-1] = last
		}
	}
	var body []string
	if listH := innerH - len(status); listH > 0 {
		body = strings.Split(m.list.View(innerW, listH, focused, now), "\n")
	}
	body = append(body, status...)
	return strings.Split(box(m.listTitle(), strings.Join(body, "\n"), w, h, m.border(focused)), "\n")
}

func (m *Model) listTitle() string {
	th := m.d.Theme
	c := m.list.Counts()
	parts := []string{th.Title.Render("Resources")}
	if c.Errors > 0 {
		parts = append(parts, th.Error.Render(fmt.Sprintf("✗ %d", c.Errors)))
	}
	if c.Warnings > 0 {
		parts = append(parts, th.Warning.Render(fmt.Sprintf("! %d", c.Warnings)))
	}
	if c.Busy > 0 {
		parts = append(parts, th.Status[tilt.StatusPending].Render(fmt.Sprintf("● %d", c.Busy)))
	}
	parts = append(parts, fmt.Sprintf("✓ %d/%d", c.Healthy, c.Total))
	for _, f := range features {
		if f.ListTitle == nil {
			continue
		}
		if s := f.ListTitle(m); s != "" {
			parts = append(parts, hookText(s))
		}
	}
	return strings.Join(parts, "  ")
}

// logPane is the boxed log view, or a feature's panel in its place. The
// body draws first because it can turn follow mode on after a reset,
// which the title shows.
func (m *Model) logPane(w, h int, focused bool) []string {
	th := m.d.Theme
	if title, body, ok := m.panel(); ok {
		return strings.Split(box(th.Title.Render(termsafe.Text(title)), body, w, h, m.border(focused)), "\n")
	}
	body := m.logs.View(max(0, w-2), max(0, h-2), focused)
	parts := []string{th.Title.Render(m.logs.Title())}
	if m.logs.Following() {
		parts = append(parts, th.Dim.Render("[follow]"))
	}
	for _, f := range features {
		if f.LogTitle == nil {
			continue
		}
		if s := f.LogTitle(m); s != "" {
			parts = append(parts, hookText(s))
		}
	}
	if len(m.d.Filters) > 0 {
		parts = append(parts, th.Dim.Render("filters: "+termsafe.Text(strings.Join(m.d.Filters, ", "))))
	}
	return strings.Split(box(strings.Join(parts, "  "), body, w, h, m.border(focused)), "\n")
}

// statusParts is the connection state, the address and the Tilt uptime
// when live. The connection error is cut to errW cells, or left out when
// errW is not positive.
func (m *Model) statusParts(now time.Time, errW int) []string {
	th := m.d.Theme
	var conn string
	switch m.conn {
	case connConnecting:
		conn = th.Status[tilt.StatusPending].Render("● connecting")
	case connLive:
		conn = th.Status[tilt.StatusHealthy].Render("● live")
	case connDown:
		text := "● disconnected"
		if m.connErr != nil && errW > 0 {
			text += ": " + ansi.Truncate(termsafe.Text(m.connErr.Error()), errW, "…")
		}
		conn = th.Status[tilt.StatusUnhealthy].Render(text)
	}
	for _, f := range features {
		if f.ConnStatus == nil {
			continue
		}
		if text, ok := f.ConnStatus(m); ok {
			conn = hookText(text)
			break
		}
	}
	parts := []string{conn, th.Dim.Render(termsafe.Text(m.d.Addr))}
	if m.showsUptime() {
		parts = append(parts, uptime(now.Sub(m.startTime)))
	}
	for _, f := range features {
		if f.StatusParts == nil {
			continue
		}
		for _, p := range f.StatusParts(m) {
			if p != "" {
				parts = append(parts, hookText(p))
			}
		}
	}
	return parts
}

// panel returns the first feature panel that is showing. Each body line
// passes through termsafe.LogLine.
func (m *Model) panel() (title, body string, ok bool) {
	for _, f := range features {
		if f.Panel == nil {
			continue
		}
		title, body, ok := f.Panel(m)
		if !ok {
			continue
		}
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			lines[i] = termsafe.LogLine(l)
		}
		return title, strings.Join(lines, "\n"), true
	}
	return "", "", false
}

// hookText makes a feature hook's text safe to draw on one line. It keeps
// SGR styling and removes every other escape sequence.
func hookText(s string) string {
	return termsafe.LogLine(strings.ReplaceAll(s, "\n", " "))
}

// remoteMarker is shown for as long as --allow-remote is set.
func (m *Model) remoteMarker() string {
	return m.d.Theme.Warning.Render("⚠ unencrypted remote")
}

// wrapParts joins parts with two spaces, starting a new row when the next
// part would pass w.
func wrapParts(parts []string, w int) []string {
	var rows []string
	cur := ""
	for _, p := range parts {
		switch {
		case cur == "":
			cur = p
		case ansi.StringWidth(cur)+2+ansi.StringWidth(p) <= w:
			cur += "  " + p
		default:
			rows = append(rows, cur)
			cur = p
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, w, "…")
	}
	return rows
}

// uptime formats d as "42s", "13m 21s" or "2h 5m".
func uptime(d time.Duration) string {
	d = max(0, d)
	h, mins, s := int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// footer is the flash while it lasts, else the key hints. The narrow log
// pane has no status line of its own, so the status leads its footer. The
// status gets the cells the flash or the core hints leave. Feature hints
// get what is left after that.
func (m *Model) footer(now time.Time) string {
	avail := m.width - 1
	flash := ""
	if m.flashActive() {
		flash = termsafe.Text(m.flash)
	}
	reserve := flash
	if reserve == "" {
		reserve = m.hints(0)
	}
	lead := ""
	if m.narrow() && m.focus == command.FocusLogs {
		lead = m.footerStatus(now, avail-ansi.StringWidth(reserve))
	}
	text := flash
	if text == "" {
		text = m.d.Theme.Dim.Render(m.hints(avail - ansi.StringWidth(lead)))
	}
	return ansi.Truncate(" "+lead+text, m.width, "…")
}

// footerStatus is the status line for the footer in at most w cells, with
// two trailing spaces. The remote marker comes first, so it is the last
// thing cut. The connection error gets only the cells left over, so it
// can't push the marker or the hints off screen.
func (m *Model) footerStatus(now time.Time, w int) string {
	lead := ""
	if m.d.Remote {
		lead = m.remoteMarker() + "  "
		w -= ansi.StringWidth(lead)
	}
	parts := m.statusParts(now, 0)
	errW := w - ansi.StringWidth(strings.Join(parts, "  ")) - len("  ") - len(": ")
	if errW > 0 {
		parts = m.statusParts(now, errW)
	}
	if w > len("  ") {
		lead += ansi.Truncate(strings.Join(parts, "  "), w-len("  "), "…") + "  "
	}
	return lead
}

// hints returns the footer hints in at most w cells where it can. The
// core hints always show. Feature hints go after the first core hint and
// are dropped from the last one back until the line fits.
func (m *Model) hints(w int) string {
	cmds := m.commands()
	label := func(h Hint) string {
		i := slices.IndexFunc(cmds, func(c command.Command) bool { return c.ID == h.ID })
		if i < 0 || len(cmds[i].Keys) == 0 {
			return ""
		}
		return cmds[i].Keys[0] + " " + h.Label
	}
	var core, extra []string
	for _, h := range footerHints {
		if s := label(h); s != "" {
			core = append(core, s)
		}
	}
	for _, f := range features {
		for _, h := range f.Hints {
			if s := label(h); s != "" {
				extra = append(extra, termsafe.Text(s))
			}
		}
	}
	at := min(1, len(core))
	for n := len(extra); ; n-- {
		line := strings.Join(slices.Concat(core[:at], extra[:n], core[at:]), " · ")
		if n == 0 || ansi.StringWidth(line) <= w {
			return line
		}
	}
}

// drawOverlay boxes o, sized to its content, and splices it centred over
// lines.
func (m *Model) drawOverlay(lines []string, o overlay.Overlay) {
	body := o.View(max(0, m.width-4), max(0, len(lines)-4))
	title := m.d.Theme.Title.Render(termsafe.Text(o.Title()))
	rows := strings.Split(body, "\n")
	contentW := ansi.StringWidth(title) + 3
	for _, r := range rows {
		contentW = max(contentW, ansi.StringWidth(r))
	}
	bw := min(contentW+2, m.width)
	bh := min(len(rows)+2, len(lines))
	boxed := box(title, body, bw, bh, m.d.Theme.BorderFocused)
	x, y := (m.width-bw)/2, (len(lines)-bh)/2
	for i, row := range strings.Split(boxed, "\n") {
		line := lines[y+i]
		left := ansi.Truncate(line, x, "")
		if lw := ansi.StringWidth(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(line, x+bw, "")
		lines[y+i] = left + sgrReset + row + sgrReset + right
	}
}

// box draws body inside a rounded border of w×h cells with title in the
// top edge: ╭─ title ───╮. It returns exactly h lines for h ≥ 1. Body
// lines are cut or padded to the inner width.
func box(title, body string, w, h int, style lipgloss.Style) string {
	if h <= 0 {
		return ""
	}
	if w < 5 {
		return strings.Join(slices.Repeat([]string{strings.Repeat(" ", max(0, w))}, h), "\n")
	}
	innerW := w - 2
	title = ansi.Truncate(title, w-5, "…")
	fill := w - 5 - ansi.StringWidth(title)
	lines := make([]string, 0, h)
	lines = append(lines, style.Render("╭─")+" "+title+sgrReset+" "+style.Render(strings.Repeat("─", fill)+"╮"))
	side := style.Render("│")
	bodyLines := strings.Split(body, "\n")
	for i := range max(0, h-2) {
		l := ""
		if i < len(bodyLines) {
			l = bodyLines[i]
		}
		if lw := ansi.StringWidth(l); lw > innerW {
			l = ansi.Truncate(l, innerW, "") + sgrReset
		} else {
			l += strings.Repeat(" ", innerW-lw)
		}
		lines = append(lines, side+l+side)
	}
	if h >= 2 {
		lines = append(lines, style.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	}
	return strings.Join(lines, "\n")
}
