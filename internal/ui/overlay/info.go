package overlay

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// ErrDepsUnavailable means nothing can fetch dependency info.
var ErrDepsUnavailable = errors.New("dependency info unavailable")

// infoLabelW is the width every detail label pads to.
const infoLabelW = len("last error")

// infoMaxDepth is how deep the dependency tree walks.
const infoMaxDepth = 8

// Info is the panel `i` shows for one resource: its details and its
// dependency tree. esc or i closes it and j/k or down/up scroll. It is a
// pointer so SetDeps can fill it in after it is pushed, and so View can
// record the height that Update scrolls against.
type Info struct {
	th      theme.Theme
	res     tilt.Resource
	deps    map[string][]string
	depsErr error
	loaded  bool // SetDeps was called
	offset  int  // first line drawn
	lastH   int  // height from the last View
}

// NewInfo returns the panel for r. Its dependency section shows
// "loading…" until SetDeps.
func NewInfo(th theme.Theme, r tilt.Resource) *Info {
	return &Info{th: th, res: r}
}

// Resource returns the raw name of the resource the panel shows.
func (i *Info) Resource() string { return i.res.Name() }

// SetDeps gives the panel every resource's dependencies, or the error
// that stopped them loading.
func (i *Info) SetDeps(deps map[string][]string, err error) {
	i.deps, i.depsErr, i.loaded = deps, err, true
}

func (i *Info) Title() string { return "Info · " + i.res.Name() }

func (i *Info) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return i, nil
	}
	switch k.String() {
	case "esc", "i":
		return i, func() tea.Msg { return CloseMsg{} }
	case "j", "down":
		i.offset = min(i.offset+1, max(0, len(i.lines())-i.lastH))
	case "k", "up":
		i.offset = max(i.offset-1, 0)
	}
	return i, nil
}

// View returns at most height lines, each at most width cells wide,
// starting at the scroll offset.
func (i *Info) View(width, height int) string {
	lines := i.lines()
	i.lastH = height
	i.offset = max(0, min(i.offset, len(lines)-height))
	end := min(len(lines), i.offset+height)
	out := make([]string, 0, max(0, end-i.offset))
	for _, l := range lines[i.offset:end] {
		out = append(out, ansi.Truncate(l, width, "…"))
	}
	return strings.Join(out, "\n")
}

// lines returns every line of the panel: the detail rows, a blank line
// and the dependency section.
func (i *Info) lines() []string {
	r := i.res
	var lines []string
	row := func(label, value string) {
		lines = append(lines, label+strings.Repeat(" ", infoLabelW-len(label))+"  "+value)
	}

	if kind := termsafe.Text(r.Kind()); kind != "" {
		row("kind", kind)
	}
	st := r.Combined()
	row("status", i.th.Status[st].Render("●")+" "+st.String())
	if r.HasWarning() {
		row("warnings", fmt.Sprint(len(r.Status.BuildHistory[0].Warnings)))
	}
	if r.IsDisabled() {
		row("disabled", "yes")
	}
	if w := r.Status.Waiting; w != nil && len(w.On) > 0 {
		names := make([]string, 0, len(w.On))
		for _, on := range w.On {
			names = append(names, termsafe.Text(on.Name))
		}
		value := strings.Join(names, ", ")
		if w.Reason != "" {
			value += "  (" + termsafe.Text(w.Reason) + ")"
		}
		row("waiting on", value)
	}
	for _, lk := range r.Status.EndpointLinks {
		value := termsafe.Text(lk.URL)
		if name := termsafe.Text(lk.Name); name != "" {
			value = name + "  " + value
		}
		row("link", value)
	}
	if k := r.Status.K8sResourceInfo; k != nil {
		restarts := "restarts"
		if k.PodRestarts == 1 {
			restarts = "restart"
		}
		row("pod", fmt.Sprintf("%s · %s · %d %s", termsafe.Text(k.PodName), termsafe.Text(k.PodStatus), k.PodRestarts, restarts))
	}
	if e := r.LastBuildError(); e != "" {
		first, _, _ := strings.Cut(e, "\n")
		row("last error", i.th.Error.Render(termsafe.Text(first)))
	}

	lines = append(lines, "")
	return append(lines, i.depLines()...)
}

// depLines returns the dependency section: a loading or error line, or
// the "depends on" tree and the "used by" list.
func (i *Info) depLines() []string {
	switch {
	case !i.loaded:
		return []string{i.th.Title.Render("depends on") + "  loading…"}
	case errors.Is(i.depsErr, tilt.ErrUnknownFormat):
		return []string{"dependency info unavailable for this Tilt version"}
	case errors.Is(i.depsErr, tilt.ErrTooLarge):
		return []string{"engine dump too large (over 64 MiB)"}
	case errors.Is(i.depsErr, ErrDepsUnavailable):
		return []string{"dependency info unavailable"}
	case i.depsErr != nil:
		return []string{"dependency info failed: " + termsafe.Text(i.depsErr.Error())}
	}

	name := i.res.Name()
	var lines []string
	if len(i.deps[name]) == 0 {
		lines = append(lines, i.th.Title.Render("depends on")+"  none")
	} else {
		lines = append(lines, i.th.Title.Render("depends on"))
		i.tree(name, []string{name}, "", &lines)
	}

	var users []string
	for user, deps := range i.deps {
		if slices.Contains(deps, name) {
			users = append(users, user)
		}
	}
	slices.Sort(users)
	if len(users) == 0 {
		return append(lines, i.th.Title.Render("used by")+"  none")
	}
	lines = append(lines, i.th.Title.Render("used by"))
	for _, u := range users {
		lines = append(lines, "  "+termsafe.Text(u))
	}
	return lines
}

// tree appends name's dependencies to out depth-first in Tilt's order.
// path holds the names from the panel's resource down to name. A name
// already on path prints as a cycle and stops there.
func (i *Info) tree(name string, path []string, prefix string, out *[]string) {
	kids := i.deps[name]
	for n, kid := range kids {
		last := n == len(kids)-1
		branch, cont := "├─ ", "│  "
		if last {
			branch, cont = "└─ ", "   "
		}
		shown := termsafe.Text(kid)
		if slices.Contains(path, kid) {
			*out = append(*out, "  "+prefix+branch+shown+" (cycle)")
			continue
		}
		*out = append(*out, "  "+prefix+branch+shown)
		if len(path) < infoMaxDepth {
			i.tree(kid, append(slices.Clip(path), kid), prefix+cont, out)
		}
	}
}
