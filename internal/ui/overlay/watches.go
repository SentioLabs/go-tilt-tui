package overlay

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// ErrNeedsLocalTilt is the panel's error when the TUI watches a remote
// Tilt and so must not run `tilt get` on this host.
var ErrNeedsLocalTilt = errors.New("file-watch info needs a local Tilt")

// watchesEvents is how many of a watch's events the panel shows.
const watchesEvents = 10

// Watches is the file-watch panel `w` shows. It says it is loading until
// SetWatches hands it the result. j/k or down/up scroll and esc or w
// closes. It is a pointer so View can record the height that Update
// scrolls against.
type Watches struct {
	th       theme.Theme
	resource string // raw resource name, "" for every watch
	ws       []tilt.FileWatch
	err      error
	loaded   bool
	offset   int // first line drawn
	lastH    int // height from the last View
}

// NewWatches returns the panel for resource's watches, or for every watch
// when resource is "".
func NewWatches(th theme.Theme, resource string) *Watches {
	return &Watches{th: th, resource: resource}
}

// SetWatches gives the panel Tilt's watches, sorted by resource and name,
// or the error that stopped them loading.
func (w *Watches) SetWatches(ws []tilt.FileWatch, err error) {
	w.ws, w.err, w.loaded = ws, err, true
}

// Title is raw: the root model draws it through termsafe.Text.
func (w *Watches) Title() string {
	if w.resource == "" {
		return "File watches · all"
	}
	return "File watches · " + w.resource
}

func (w *Watches) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return w, nil
	}
	switch k.String() {
	case "esc", "w":
		return w, func() tea.Msg { return CloseMsg{} }
	case "j", "down":
		// The line count doesn't depend on the width.
		w.offset = min(w.offset+1, max(0, len(w.lines(0))-w.lastH))
	case "k", "up":
		w.offset = max(w.offset-1, 0)
	}
	return w, nil
}

// View returns at most height lines, each at most width cells wide,
// starting at the scroll offset.
func (w *Watches) View(width, height int) string {
	lines := w.lines(width)
	w.lastH = height
	w.offset = max(0, min(w.offset, len(lines)-height))
	end := min(len(lines), w.offset+height)
	return strings.Join(lines[w.offset:end], "\n")
}

// lines returns the whole panel, each line cut to width cells.
func (w *Watches) lines(width int) []string {
	var out []string
	switch {
	case !w.loaded:
		out = []string{"loading file watches…"}
	case errors.Is(w.err, ErrNeedsLocalTilt):
		out = []string{ErrNeedsLocalTilt.Error()}
	case w.err != nil:
		out = []string{"file watches failed: " + termsafe.Text(w.err.Error())}
	default:
		out = w.watchLines()
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, width, "…")
	}
	return out
}

// watchLines lists the shown watches with a blank line between them.
func (w *Watches) watchLines() []string {
	var out []string
	for _, fw := range w.ws {
		if w.resource != "" && fw.Resource != w.resource {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, w.watch(fw)...)
	}
	if len(out) > 0 {
		return out
	}
	if w.resource == "" {
		return []string{"no file watches"}
	}
	return []string{"no file watches for " + termsafe.Text(w.resource)}
}

// watch returns one watch's lines: its name, then its paths, ignores,
// newest events and error.
func (w *Watches) watch(fw tilt.FileWatch) []string {
	name := termsafe.Text(fw.Name)
	if w.resource == "" && fw.Resource != "" {
		name = termsafe.Text(fw.Resource) + " · " + name
	}
	out := []string{w.th.Title.Render(name)}

	var paths []string
	for _, p := range fw.WatchedPaths {
		paths = append(paths, termsafe.Text(p))
	}
	out = append(out, watchesRows("watching", paths)...)

	var ignores []string
	for _, ig := range fw.Ignores {
		row := termsafe.Text(ig.BasePath)
		if len(ig.Patterns) > 0 {
			pats := make([]string, len(ig.Patterns))
			for i, p := range ig.Patterns {
				pats[i] = termsafe.Text(p)
			}
			row += ": " + strings.Join(pats, ", ")
		}
		ignores = append(ignores, row)
	}
	out = append(out, watchesRows("ignoring", ignores)...)

	events := slices.Clone(fw.Events)
	slices.SortStableFunc(events, func(a, b tilt.FileEvent) int { return b.Time.Compare(a.Time) })
	var rows []string
	for _, ev := range events[:min(len(events), watchesEvents)] {
		row := ev.Time.Local().Format("15:04:05")
		if files := watchesSeen(ev.SeenFiles); len(files) > 0 {
			row += "  " + strings.Join(files, ", ")
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = []string{"none"}
	}
	out = append(out, watchesRows("events", rows)...)

	if fw.Error != "" {
		out = append(out, watchesRows("error", []string{w.th.Error.Render(termsafe.Text(fw.Error))})...)
	}
	return out
}

// watchesRows labels the first row and indents the rest under it.
func watchesRows(label string, rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		if i == 0 {
			out[i] = "  " + label + strings.Repeat(" ", 8-len(label)) + "  " + r
		} else {
			out[i] = strings.Repeat(" ", 12) + r
		}
	}
	return out
}

// watchesSeen returns files through termsafe.Text without repeats, in
// first-seen order.
func watchesSeen(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, termsafe.Text(f))
	}
	return out
}
