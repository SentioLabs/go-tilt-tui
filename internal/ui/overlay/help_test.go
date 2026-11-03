package overlay

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

func TestHelpListsEnabledCommands(t *testing.T) {
	th, _ := theme.ByName("mono")
	never := func(command.Context) bool { return false }
	cmds := []command.Command{
		{ID: "quit", Title: "quit", Keys: []string{"q"}, Scope: command.ScopeGlobal},
		{ID: "hidden", Title: "secret action", Keys: []string{"z"}, Scope: command.ScopeGlobal, Enabled: never},
		{ID: "follow", Title: "follow on/off", Keys: []string{"s"}, Scope: command.ScopeLogs},
	}
	h := NewHelp(cmds, command.Context{}, th)
	out := ansi.Strip(h.View(60, 20))
	for _, want := range []string{"Global", "q", "quit", "Logs", "follow on/off"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret action") {
		t.Error("help lists a disabled command")
	}
	if h.Title() != "Help" {
		t.Errorf("Title = %q", h.Title())
	}
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: '?', Text: "?"}} {
		_, cmd := h.Update(k)
		if cmd == nil {
			t.Fatalf("%s did not close help", k.String())
		}
		if _, ok := cmd().(CloseMsg); !ok {
			t.Fatalf("%s did not send CloseMsg", k.String())
		}
	}
}

func TestHelpScrollsWhenTallerThanScreen(t *testing.T) {
	th, _ := theme.ByName("mono")
	var cmds []command.Command
	for _, k := range "abcdefghij" {
		cmds = append(cmds, command.Command{Title: "do " + string(k), Keys: []string{string(k)}, Scope: command.ScopeGlobal})
	}
	h := NewHelp(cmds, command.Context{}, th)
	first := ansi.Strip(h.View(40, 4))
	if got := strings.Count(first, "\n") + 1; got != 4 {
		t.Fatalf("View(40, 4) has %d lines:\n%s", got, first)
	}
	if !strings.Contains(first, "Global") {
		t.Fatalf("help does not start at the top:\n%s", first)
	}
	for range 20 {
		h, _ = h.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	last := ansi.Strip(h.View(40, 4))
	if !strings.Contains(last, "do j") || strings.Contains(last, "Global") {
		t.Fatalf("j did not scroll to the end:\n%s", last)
	}
	h, _ = h.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if back := ansi.Strip(h.View(40, 4)); strings.Contains(back, "do j") {
		t.Fatalf("k after the end did not scroll up one line:\n%s", back)
	}
}
