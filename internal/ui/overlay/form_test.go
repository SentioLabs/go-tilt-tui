package overlay

import (
	"maps"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ func(theme.Theme, string, []tilt.Button) Overlay = NewButtons
	_ func(theme.Theme, tilt.Button) Overlay           = NewForm
	_ func(tilt.Button) bool                           = NeedsForm
	_                                                  = ClickMsg{Button: tilt.Button{}, Values: map[string]any{}}
)

// --- Helpers ---

func formTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return th
}

// formKey builds the key press whose String() is s.
func formKey(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// formMsgs runs cmd and every command in a batch, and returns the
// messages they produce.
func formMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, formMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// formPress sends keys to o in order. It returns the overlay that is on
// top afterwards and the messages the last key produced.
func formPress(o Overlay, keys ...string) (Overlay, []tea.Msg) {
	var msgs []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		o, cmd = o.Update(formKey(k))
		msgs = formMsgs(cmd)
	}
	return o, msgs
}

// formClicks returns the ClickMsgs in msgs and whether a CloseMsg was there.
func formClicks(msgs []tea.Msg) (out []ClickMsg, closed bool) {
	for _, m := range msgs {
		switch m := m.(type) {
		case ClickMsg:
			out = append(out, m)
		case CloseMsg:
			closed = true
		}
	}
	return out, closed
}

func formButton(inputs ...tilt.InputSpec) tilt.Button {
	return tilt.Button{
		Metadata: tilt.ObjectMeta{Name: "api-deploy", ResourceVersion: "7"},
		Spec:     tilt.ButtonSpec{Text: "Deploy", Inputs: inputs},
	}
}

var (
	formText   = tilt.InputSpec{Name: "msg", Label: "Message", Text: &tilt.TextInputSpec{DefaultValue: "v1", Placeholder: "why"}}
	formBool   = tilt.InputSpec{Name: "force", Label: "Force", Bool: &tilt.BoolInputSpec{}}
	formChoice = tilt.InputSpec{Name: "env", Label: "Env", Choice: &tilt.ChoiceInputSpec{Choices: []string{"dev", "staging", "prod"}}}
	formHidden = tilt.InputSpec{Name: "token", Hidden: &tilt.HiddenInputSpec{Value: "s3cret"}}
)

// --- Behavior tests ---

func TestFormSendsEachInputType(t *testing.T) {
	cases := []struct {
		name  string
		input tilt.InputSpec
		keys  []string
		want  map[string]any
	}{
		{"text default", formText, nil, map[string]any{"msg": "v1"}},
		{"text typed", formText, []string{"x", "j", "space", "q"}, map[string]any{"msg": "v1xj q"}},
		{"bool default", formBool, nil, map[string]any{"force": false}},
		{"bool toggled", formBool, []string{"space"}, map[string]any{"force": true}},
		{"bool toggled twice", formBool, []string{"space", "space"}, map[string]any{"force": false}},
		{"choice default", formChoice, nil, map[string]any{"env": "dev"}},
		{"choice right", formChoice, []string{"right"}, map[string]any{"env": "staging"}},
		{"choice j j", formChoice, []string{"j", "j"}, map[string]any{"env": "prod"}},
		{"choice wraps left", formChoice, []string{"left"}, map[string]any{"env": "prod"}},
		{"choice k wraps", formChoice, []string{"j", "k", "k"}, map[string]any{"env": "prod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := NewForm(formTheme(t), formButton(tc.input))
			o, _ = formPress(o, tc.keys...)
			_, msgs := formPress(o, "enter")
			got, closed := formClicks(msgs)
			if len(got) != 1 || !closed {
				t.Fatalf("enter gave clicks %v closed %v", got, closed)
			}
			if !maps.Equal(got[0].Values, tc.want) {
				t.Fatalf("values = %#v, want %#v", got[0].Values, tc.want)
			}
			if got[0].Button.Metadata.ResourceVersion != "7" {
				t.Fatalf("clicked resourceVersion %q", got[0].Button.Metadata.ResourceVersion)
			}
		})
	}
}

func TestFormAllInputsTogether(t *testing.T) {
	o := NewForm(formTheme(t), formButton(formText, formHidden, formBool, formChoice))
	// msg is focused first. tab skips the hidden input.
	o, _ = formPress(o, "!", "tab", "space", "tab", "right", "shift+tab", "up", "x")
	_, msgs := formPress(o, "enter")
	got, _ := formClicks(msgs)
	want := map[string]any{"msg": "v1!x", "token": "s3cret", "force": true, "env": "staging"}
	if len(got) != 1 || !maps.Equal(got[0].Values, want) {
		t.Fatalf("clicks = %#v, want values %#v", got, want)
	}
}

func TestFormHiddenInputNotDrawn(t *testing.T) {
	o := NewForm(formTheme(t), formButton(formText, formHidden))
	view := ansi.Strip(o.View(60, 10))
	if strings.Contains(view, "token") || strings.Contains(view, "s3cret") {
		t.Fatalf("hidden input drawn:\n%s", view)
	}
	for _, want := range []string{"› Message: v1", "enter submit · esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("form lacks %q:\n%s", want, view)
		}
	}
}

func TestFormView(t *testing.T) {
	o := NewForm(formTheme(t), formButton(formText, formBool, formChoice))
	o, _ = formPress(o, "tab", "space")
	got := strings.Split(ansi.Strip(o.View(60, 10)), "\n")
	if w := ansi.StringWidth(got[0]); w != 2+formTextWidth {
		t.Fatalf("text row is %d wide, want %d", w, 2+formTextWidth)
	}
	for i, l := range got {
		got[i] = strings.TrimRight(l, " ")
	}
	want := []string{
		"  Message: v1",
		"› [x] Force",
		"  Env: ‹ dev ›",
		"",
		"tab next · enter submit · esc cancel",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("view =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFormEscCancels(t *testing.T) {
	o := NewForm(formTheme(t), formButton(formText))
	_, msgs := formPress(o, "x", "esc")
	got, closed := formClicks(msgs)
	if len(got) != 0 || !closed {
		t.Fatalf("esc gave clicks %v closed %v", got, closed)
	}
}

func TestFormConfirmation(t *testing.T) {
	b := formButton(formBool)
	b.Spec.RequiresConfirmation = true

	for _, cancel := range []string{"n", "esc"} {
		o := NewForm(formTheme(t), b)
		o, msgs := formPress(o, "space", "enter")
		if got, closed := formClicks(msgs); len(got) != 0 || closed {
			t.Fatalf("enter clicked before the y/n step: %v %v", got, closed)
		}
		if v := ansi.Strip(o.View(60, 10)); v != "Deploy? y/n" {
			t.Fatalf("confirm view = %q", v)
		}
		_, msgs = formPress(o, cancel)
		if got, closed := formClicks(msgs); len(got) != 0 || !closed {
			t.Fatalf("%s gave clicks %v closed %v, want no click and a close", cancel, got, closed)
		}
	}

	o := NewForm(formTheme(t), b)
	o, _ = formPress(o, "space", "enter", "x") // x is neither y nor n
	_, msgs := formPress(o, "y")
	got, closed := formClicks(msgs)
	if len(got) != 1 || !closed || !maps.Equal(got[0].Values, map[string]any{"force": true}) {
		t.Fatalf("y gave clicks %#v closed %v", got, closed)
	}
}

func TestFormWithoutVisibleInputsOpensOnConfirm(t *testing.T) {
	b := formButton(formHidden)
	b.Spec.RequiresConfirmation = true
	if !NeedsForm(b) {
		t.Fatal("NeedsForm false for a button that requires confirmation")
	}
	o := NewForm(formTheme(t), b)
	if v := ansi.Strip(o.View(60, 10)); v != "Deploy? y/n" {
		t.Fatalf("view = %q, want the y/n step", v)
	}
	_, msgs := formPress(o, "y")
	got, _ := formClicks(msgs)
	if len(got) != 1 || !maps.Equal(got[0].Values, map[string]any{"token": "s3cret"}) {
		t.Fatalf("clicks = %#v", got)
	}
}

func TestNeedsForm(t *testing.T) {
	cases := []struct {
		b    tilt.Button
		want bool
	}{
		{formButton(), false},
		{formButton(formHidden), false},
		{formButton(formText), true},
		{formButton(formBool, formHidden), true},
	}
	for i, tc := range cases {
		if got := NeedsForm(tc.b); got != tc.want {
			t.Errorf("case %d: NeedsForm = %v, want %v", i, got, tc.want)
		}
	}
}

func TestFormEmptyChoiceLeftOut(t *testing.T) {
	empty := tilt.InputSpec{Name: "none", Choice: &tilt.ChoiceInputSpec{}}
	o := NewForm(formTheme(t), formButton(empty, formBool))
	_, msgs := formPress(o, "enter")
	got, _ := formClicks(msgs)
	if len(got) != 1 || !maps.Equal(got[0].Values, map[string]any{"force": false}) {
		t.Fatalf("clicks = %#v, want only force", got)
	}
}

var formSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestFormFiltersTiltText(t *testing.T) {
	evil := "\x1b]52;c;ZXZpbA==\x07"
	b := tilt.Button{
		Metadata: tilt.ObjectMeta{Name: "x"},
		Spec: tilt.ButtonSpec{Text: "Run" + evil, RequiresConfirmation: true, Inputs: []tilt.InputSpec{
			{Name: "a", Label: "Label" + evil, Text: &tilt.TextInputSpec{DefaultValue: "def" + evil, Placeholder: "ph" + evil}},
			{Name: "b", Label: "Flag" + evil, Bool: &tilt.BoolInputSpec{}},
			{Name: "c", Label: "Pick" + evil, Choice: &tilt.ChoiceInputSpec{Choices: []string{"one" + evil}}},
		}},
	}
	o := NewForm(formTheme(t), b)
	views := []string{o.View(80, 10), o.Title()}
	o, _ = formPress(o, "enter")
	views = append(views, o.View(80, 10))
	list := NewButtons(formTheme(t), "api"+evil, []tilt.Button{b})
	views = append(views, list.View(80, 10), list.Title())
	for _, v := range views {
		if rest := formSGR.ReplaceAllString(v, ""); strings.ContainsAny(rest, "\x1b\x07") {
			t.Fatalf("escape sequence reached the view: %q", v)
		}
	}
}
