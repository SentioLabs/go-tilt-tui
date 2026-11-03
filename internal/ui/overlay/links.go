package overlay

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// linksWidth is the widest the links menu grows.
const linksWidth = 72

// Links is the menu `o` shows when a resource has several endpoint
// links. j/k or up/down move, enter or a digit opens and esc closes.
type Links struct {
	th       theme.Theme
	resource string // termsafe.Text of the resource name
	links    []tilt.EndpointLink
	open     func(url string) tea.Cmd
	cursor   int
}

// NewLinks returns the menu for resource's links, in Tilt's order.
// Choosing a link runs the command open returns for its raw URL and
// closes the menu. The opener checks the URL's scheme.
func NewLinks(th theme.Theme, resource string, links []tilt.EndpointLink, open func(url string) tea.Cmd) *Links {
	return &Links{th: th, resource: termsafe.Text(resource), links: links, open: open}
}

func (l *Links) Title() string { return "Open link · " + l.resource }

func (l *Links) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return l, nil
	}
	switch s := k.String(); s {
	case "esc":
		return l, func() tea.Msg { return CloseMsg{} }
	case "j", "down":
		l.cursor = min(l.cursor+1, len(l.links)-1)
	case "k", "up":
		l.cursor = max(l.cursor-1, 0)
	case "enter":
		return l, l.choose(l.cursor)
	default:
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if i := int(s[0] - '1'); i < len(l.links) {
				return l, l.choose(i)
			}
		}
	}
	return l, nil
}

func (l *Links) choose(i int) tea.Cmd {
	if i < 0 || i >= len(l.links) {
		return nil
	}
	return tea.Batch(l.open(l.links[i].URL), func() tea.Msg { return CloseMsg{} })
}

// View lists the links as "<n> <name>  <url>" in at most width cells.
// A link without a name shows only its URL.
func (l *Links) View(width, height int) string {
	w := min(width, linksWidth)
	start := max(0, min(l.cursor-height+1, len(l.links)-height))
	var out []string
	for i := start; i < len(l.links) && len(out) < height; i++ {
		lk := l.links[i]
		num := " "
		if i < 9 {
			num = fmt.Sprint(i + 1)
		}
		text := num + " " + termsafe.Text(lk.URL)
		if name := termsafe.Text(lk.Name); name != "" {
			text = num + " " + name + "  " + termsafe.Text(lk.URL)
		}
		text = ansi.Truncate(text, w, "…")
		if i == l.cursor {
			text += strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
			text = l.th.Selected.Render(text)
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n")
}
