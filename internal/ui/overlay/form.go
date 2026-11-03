package overlay

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// formTextWidth caps a text input's width, prompt included. The overlay
// sizes itself to its widest row, and a text field pads to its width.
const formTextWidth = 48

// formField is one input of a button form. Exactly one of text, isBool,
// choices or hidden applies, following the input's spec.
type formField struct {
	name    string
	label   string
	text    *TextField
	isBool  bool
	on      bool
	choices []string
	choice  int
	hidden  bool
	value   string // a hidden input's value
}

// form collects a button's input values and asks for confirmation when
// the button requires it. Hidden inputs are never drawn but are always
// sent.
type form struct {
	th      theme.Theme
	button  tilt.Button
	fields  []*formField
	visible []*formField
	focus   int  // index into visible
	confirm bool // showing the y/n step
}

// NeedsForm reports whether clicking b needs a form: b has an input the
// user can see, or it requires confirmation.
func NeedsForm(b tilt.Button) bool {
	if b.Spec.RequiresConfirmation {
		return true
	}
	for _, in := range b.Spec.Inputs {
		if in.Hidden == nil {
			return true
		}
	}
	return false
}

// NewForm returns the form for b. A button with no visible input opens
// on the confirmation step.
func NewForm(th theme.Theme, b tilt.Button) Overlay {
	f := newForm(th, b)
	if len(f.visible) == 0 {
		f.confirm = true
	}
	return f
}

func newForm(th theme.Theme, b tilt.Button) *form {
	f := &form{th: th, button: b}
	for _, in := range b.Spec.Inputs {
		fd := &formField{name: in.Name, label: termsafe.Text(in.Label)}
		if fd.label == "" {
			fd.label = termsafe.Text(in.Name)
		}
		switch {
		case in.Hidden != nil:
			fd.hidden, fd.value = true, in.Hidden.Value
		case in.Bool != nil:
			fd.isBool, fd.on = true, in.Bool.DefaultValue
		case in.Choice != nil:
			if len(in.Choice.Choices) == 0 {
				// Nothing to pick. Leaving the input out of the
				// values keeps its current value.
				continue
			}
			fd.choices = in.Choice.Choices
		default:
			placeholder, value := "", ""
			if in.Text != nil {
				placeholder, value = in.Text.Placeholder, in.Text.DefaultValue
			}
			fd.text = NewTextField(th, fd.label+": ", placeholder)
			fd.text.SetValue(value)
		}
		f.fields = append(f.fields, fd)
		if !fd.hidden {
			f.visible = append(f.visible, fd)
		}
	}
	f.setFocus(0)
	return f
}

func (f *form) Title() string { return buttonText(f.button) }

// values returns what ClickButton sends: every input by name, with the
// hidden ones at their spec value.
func (f *form) values() map[string]any {
	out := make(map[string]any, len(f.fields))
	for _, fd := range f.fields {
		switch {
		case fd.hidden:
			out[fd.name] = fd.value
		case fd.isBool:
			out[fd.name] = fd.on
		case fd.choices != nil:
			out[fd.name] = fd.choices[fd.choice]
		default:
			out[fd.name] = fd.text.Value()
		}
	}
	return out
}

func (f *form) setFocus(i int) {
	if len(f.visible) == 0 {
		return
	}
	f.focus = (i + len(f.visible)) % len(f.visible)
	for j, fd := range f.visible {
		if fd.text == nil {
			continue
		}
		if j == f.focus {
			fd.text.Focus()
		} else {
			fd.text.Blur()
		}
	}
}

func (f *form) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if f.confirm {
		return f.updateConfirm(msg)
	}
	var cur *formField
	if len(f.visible) > 0 {
		cur = f.visible[f.focus]
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		// A terminal paste goes to the focused text field.
		if p, isPaste := msg.(tea.PasteMsg); isPaste && cur != nil && cur.text != nil {
			return f, cur.text.Update(p)
		}
		return f, nil
	}
	switch k.String() {
	case "esc":
		return f, closeButtons
	case "enter":
		if f.button.Spec.RequiresConfirmation {
			f.confirm = true
			return f, nil
		}
		return f, submitClick(f.button, f.values())
	case "tab", "down":
		f.setFocus(f.focus + 1)
		return f, nil
	case "shift+tab", "up":
		f.setFocus(f.focus - 1)
		return f, nil
	}
	switch {
	case cur == nil:
	case cur.text != nil:
		return f, cur.text.Update(k)
	case cur.isBool:
		if k.String() == "space" {
			cur.on = !cur.on
		}
	case cur.choices != nil:
		switch k.String() {
		case "right", "j", "l":
			cur.choice = (cur.choice + 1) % len(cur.choices)
		case "left", "k", "h":
			cur.choice = (cur.choice - 1 + len(cur.choices)) % len(cur.choices)
		}
	}
	return f, nil
}

// updateConfirm handles the y/n step. Anything but y cancels without a
// click.
func (f *form) updateConfirm(msg tea.Msg) (Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, nil
	}
	switch k.String() {
	case "y", "Y":
		return f, submitClick(f.button, f.values())
	case "n", "N", "esc", "q":
		return f, closeButtons
	}
	return f, nil
}

// View draws one row per visible input, then the key hints. The y/n step
// replaces the inputs with the question.
func (f *form) View(width, height int) string {
	var rows []string
	if f.confirm {
		rows = []string{buttonText(f.button) + "? y/n"}
	} else {
		for i, fd := range f.visible {
			mark := "  "
			if i == f.focus {
				mark = "› "
			}
			rows = append(rows, mark+f.fieldView(fd, max(1, min(width-2, formTextWidth))))
		}
		rows = append(rows, "", f.th.Dim.Render("tab next · enter submit · esc cancel"))
	}
	if height > 0 && len(rows) > height {
		rows = rows[:height]
	}
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, width, "…")
	}
	return strings.Join(rows, "\n")
}

func (f *form) fieldView(fd *formField, width int) string {
	switch {
	case fd.isBool:
		box := "[ ]"
		if fd.on {
			box = "[x]"
		}
		return box + " " + fd.label
	case fd.choices != nil:
		return f.th.Title.Render(fd.label+":") + " ‹ " + termsafe.Text(fd.choices[fd.choice]) + " ›"
	default:
		return fd.text.View(width)
	}
}

// submitClick closes the overlay and asks the root model to click b.
func submitClick(b tilt.Button, values map[string]any) tea.Cmd {
	return tea.Batch(closeButtons, func() tea.Msg { return ClickMsg{Button: b, Values: values} })
}
