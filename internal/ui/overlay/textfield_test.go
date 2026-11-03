package overlay

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// --- Contract assertions ---

var (
	_ func(theme.Theme, string, string) *TextField = NewTextField
	_ func(*TextField) string                      = (*TextField).Value
	_ func(*TextField, string)                     = (*TextField).SetValue
	_ func(*TextField)                             = (*TextField).Focus
	_ func(*TextField)                             = (*TextField).Blur
	_ func(*TextField) bool                        = (*TextField).Focused
	_ func(*TextField, tea.Msg) tea.Cmd            = (*TextField).Update
	_ func(*TextField, int) string                 = (*TextField).View
)

// --- Behavior tests ---

func field(t *testing.T) *TextField {
	t.Helper()
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	return NewTextField(th, "/", "search logs")
}

func typeText(f *TextField, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestTextFieldEditsWhenFocused(t *testing.T) {
	f := field(t)
	typeText(f, "ignored")
	if f.Value() != "" {
		t.Fatalf("unfocused field took keys: %q", f.Value())
	}
	f.Focus()
	if !f.Focused() {
		t.Fatal("Focus did not focus")
	}
	typeText(f, "apix")
	if cmd := f.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); cmd != nil {
		t.Error("backspace returned a command")
	}
	if f.Value() != "api" {
		t.Fatalf("Value = %q, want %q", f.Value(), "api")
	}
	f.Update(tea.PasteMsg{Content: "-server"})
	if f.Value() != "api-server" {
		t.Fatalf("paste: Value = %q", f.Value())
	}
	f.Blur()
	typeText(f, "z")
	if f.Value() != "api-server" {
		t.Fatalf("blurred field took keys: %q", f.Value())
	}
}

func TestTextFieldIgnoresCtrlV(t *testing.T) {
	f := field(t)
	f.Focus()
	if cmd := f.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatal("ctrl+v returned a command, which would read the clipboard")
	}
	if f.Value() != "" {
		t.Fatalf("ctrl+v changed the value: %q", f.Value())
	}
}

func TestTextFieldOwnerHandlesEnterAndEsc(t *testing.T) {
	f := field(t)
	f.Focus()
	typeText(f, "ok")
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
		if cmd := f.Update(k); cmd != nil {
			t.Errorf("%s returned a command", k.String())
		}
	}
	if f.Value() != "ok" {
		t.Fatalf("enter or esc changed the value: %q", f.Value())
	}
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestTextFieldView(t *testing.T) {
	f := field(t)
	if got := ansi.Strip(f.View(30)); !strings.HasPrefix(got, "/search logs") {
		t.Fatalf("empty View = %q, want the prompt then the placeholder", got)
	}
	f.Focus()
	f.SetValue("evil\x1b]52;c;ZXZpbA==\x07\tname")
	if f.Value() != "evil name" {
		t.Fatalf("SetValue kept control bytes: %q", f.Value())
	}
	if rest := sgr.ReplaceAllString(f.View(30), ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("View has a non-SGR escape: %q", rest)
	}
	if got := ansi.Strip(f.View(30)); !strings.HasPrefix(got, "/evil name") {
		t.Fatalf("View = %q", got)
	}
}

func TestTextFieldViewScrollsToCursor(t *testing.T) {
	f := field(t)
	f.Focus()
	typeText(f, "abcdefghijklmnopqrstuvwxyz")
	for _, w := range []int{1, 5, 12, 40} {
		if got := ansi.StringWidth(f.View(w)); got > w {
			t.Errorf("View(%d) is %d cells wide", w, got)
		}
	}
	got := ansi.Strip(f.View(12))
	if !strings.Contains(got, "xyz") || strings.Contains(got, "abc") {
		t.Fatalf("View(12) = %q, want the end of the value", got)
	}
}
