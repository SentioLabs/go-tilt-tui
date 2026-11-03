package overlay

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// TextField is the one-line text input every P2 overlay uses. It wraps
// bubbles' textinput with a steady cursor and the theme's styles. It
// handles editing keys only: the overlay that owns it decides what enter
// and esc do.
type TextField struct {
	in textinput.Model
}

// NewTextField returns an empty, unfocused field. The prompt is drawn
// before the value. The placeholder shows while the value is empty. Both
// pass through termsafe.Text.
func NewTextField(th theme.Theme, prompt, placeholder string) *TextField {
	in := textinput.New()
	in.Prompt = termsafe.Text(prompt)
	in.Placeholder = termsafe.Text(placeholder)
	// ctrl+v would read the clipboard by running xclip or pbpaste.
	// Only internal/proc may start processes, so terminal paste is the
	// one way to paste.
	in.KeyMap.Paste.SetEnabled(false)
	plain := lipgloss.NewStyle()
	state := textinput.StyleState{Text: plain, Placeholder: th.Dim, Suggestion: th.Dim, Prompt: th.Title}
	in.SetStyles(textinput.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textinput.CursorStyle{Shape: tea.CursorBlock},
	})
	return &TextField{in: in}
}

// Value returns the text as typed.
func (f *TextField) Value() string { return f.in.Value() }

// SetValue replaces the text and puts the cursor at its end. s passes
// through termsafe.Text, because defaults come from Tilt.
func (f *TextField) SetValue(s string) {
	f.in.SetValue(termsafe.Text(s))
	f.in.CursorEnd()
}

// Focus shows the cursor and lets Update edit the text.
func (f *TextField) Focus() { f.in.Focus() }

// Blur hides the cursor. Update ignores keys until Focus.
func (f *TextField) Blur() { f.in.Blur() }

func (f *TextField) Focused() bool { return f.in.Focused() }

// Update applies an editing key or a terminal paste. It returns nil for
// every message in practice: the cursor never blinks and ctrl+v is off.
func (f *TextField) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	f.in, cmd = f.in.Update(msg)
	return cmd
}

// View draws the prompt and value in at most width cells. A value wider
// than that scrolls to keep the cursor in view.
func (f *TextField) View(width int) string {
	f.in.SetWidth(max(1, width-ansi.StringWidth(f.in.Prompt)-1))
	f.in.SetCursor(f.in.Position())
	return ansi.Truncate(f.in.View(), max(0, width), "")
}
