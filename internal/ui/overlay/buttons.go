package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// ClickMsg asks the root model to click Button with Values. The map key
// is the input's name. Text, choice and hidden inputs carry a string and
// bool inputs carry a bool.
type ClickMsg struct {
	Button tilt.Button
	Values map[string]any
}

// buttons lists one resource's UI buttons. enter on an enabled button
// clicks it, or opens its form when it has inputs or asks to be confirmed.
type buttons struct {
	th       theme.Theme
	resource string
	items    []tilt.Button
	cursor   int
}

// NewButtons returns the button list for resource. items come from
// Store.Buttons and keep their order.
func NewButtons(th theme.Theme, resource string, items []tilt.Button) Overlay {
	return &buttons{th: th, resource: resource, items: items}
}

func (b *buttons) Title() string { return "Buttons: " + termsafe.Text(b.resource) }

func (b *buttons) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	switch k.String() {
	case "esc", "q":
		return b, closeButtons
	case "j", "down":
		b.cursor = min(b.cursor+1, max(0, len(b.items)-1))
	case "k", "up":
		b.cursor = max(b.cursor-1, 0)
	case "enter":
		if len(b.items) == 0 || b.items[b.cursor].Spec.Disabled {
			return b, nil
		}
		it := b.items[b.cursor]
		if !NeedsForm(it) {
			return b, submitClick(it, newForm(b.th, it).values())
		}
		return NewForm(b.th, it), nil
	}
	return b, nil
}

// View draws one row per button with a marker on the cursor row. A
// disabled button is dimmed and says so.
func (b *buttons) View(width, height int) string {
	if len(b.items) == 0 {
		return b.th.Dim.Render("no buttons")
	}
	rows := make([]string, 0, len(b.items)+2)
	for i, it := range b.items {
		mark := "  "
		if i == b.cursor {
			mark = "› "
		}
		text := mark + buttonText(it)
		if it.Spec.Disabled {
			text = b.th.Dim.Render(text + " (disabled)")
		} else if i == b.cursor {
			text = b.th.Selected.Render(text)
		}
		rows = append(rows, text)
	}
	rows = append(rows, "", b.th.Dim.Render("enter click · esc close"))
	start := 0
	if height > 0 && b.cursor >= height {
		start = b.cursor - height + 1
	}
	end := len(rows)
	if height > 0 {
		end = min(end, start+height)
	}
	out := make([]string, 0, end-start)
	for _, r := range rows[start:end] {
		out = append(out, ansi.Truncate(r, width, "…"))
	}
	return strings.Join(out, "\n")
}

// buttonText is the label Tilt shows, or the button's name when the text
// is empty.
func buttonText(b tilt.Button) string {
	if b.Spec.Text != "" {
		return termsafe.Text(b.Spec.Text)
	}
	return termsafe.Text(b.Metadata.Name)
}

func closeButtons() tea.Msg { return CloseMsg{} }
