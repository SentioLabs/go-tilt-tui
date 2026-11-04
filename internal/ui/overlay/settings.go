package overlay

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// Setting is one row of the settings pane.
type Setting struct {
	Key     string   // config key, e.g. "mouseWheelList"
	Label   string   // e.g. "Mouse wheel over list"
	Options []string // values in cycle order
	Help    []string // one line per option, same order
	Value   string   // current value; the first option when not in Options
}

// SettingMsg reports a changed setting. The pane stays open.
type SettingMsg struct{ Key, Value string }

// settings lists settings and cycles the one under the cursor through its
// options. It only reports changes and never stores them.
type settings struct {
	th     theme.Theme
	items  []Setting
	index  []int // per item, the position of its current value in Options
	cursor int
}

// NewSettings returns the settings pane for items, in order.
func NewSettings(th theme.Theme, items []Setting) Overlay {
	index := make([]int, len(items))
	for i, it := range items {
		index[i] = max(0, slices.Index(it.Options, it.Value))
	}
	return &settings{th: th, items: items, index: index}
}

func (s *settings) Title() string { return "Settings" }

func (s *settings) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch k.String() {
	case "esc", "q":
		return s, closeSettings
	case "j", "down":
		s.cursor = min(s.cursor+1, max(0, len(s.items)-1))
	case "k", "up":
		s.cursor = max(s.cursor-1, 0)
	case "enter", "space", "l", "right":
		return s, s.step(1)
	case "h", "left":
		return s, s.step(-1)
	}
	return s, nil
}

// step moves the cursor row's value by delta options, wrapping at both
// ends, and returns a command that reports the new value.
func (s *settings) step(delta int) tea.Cmd {
	if s.cursor >= len(s.items) {
		return nil
	}
	it := s.items[s.cursor]
	n := len(it.Options)
	if n == 0 {
		return nil
	}
	s.index[s.cursor] = (s.index[s.cursor] + delta + n) % n
	msg := SettingMsg{Key: it.Key, Value: it.Options[s.index[s.cursor]]}
	return func() tea.Msg { return msg }
}

// value is the text shown for item i.
func (s *settings) value(i int) string {
	if opts := s.items[i].Options; len(opts) > 0 {
		return opts[s.index[i]]
	}
	return s.items[i].Value
}

// View draws one row per setting with a marker on the cursor row, then
// the cursor row's help line for its current value.
func (s *settings) View(width, height int) string {
	rows := make([]string, 0, len(s.items)+3)
	for i, it := range s.items {
		mark := "  "
		if i == s.cursor {
			mark = "› "
		}
		text := mark + termsafe.Text(it.Label) + "  ‹ " + termsafe.Text(s.value(i)) + " ›"
		if i == s.cursor {
			text = s.th.Selected.Render(text)
		}
		rows = append(rows, text)
	}
	if s.cursor < len(s.items) {
		if help := s.items[s.cursor].Help; s.index[s.cursor] < len(help) {
			rows = append(rows, s.th.Dim.Render("  "+termsafe.Text(help[s.index[s.cursor]])))
		}
	}
	rows = append(rows, "", s.th.Dim.Render("enter/←→ change · esc close"))
	if height > 0 && len(rows) > height {
		rows = rows[:height]
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, ansi.Truncate(r, width, "…"))
	}
	return strings.Join(out, "\n")
}

func closeSettings() tea.Msg { return CloseMsg{} }
