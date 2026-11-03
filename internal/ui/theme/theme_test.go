package theme

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// --- Contract assertions ---

var (
	_ func(string) (Theme, bool)   = ByName
	_ func(string) (Theme, string) = Select
)

var allStatuses = []tilt.Status{
	tilt.StatusNone, tilt.StatusPending, tilt.StatusBuilding,
	tilt.StatusHealthy, tilt.StatusUnhealthy, tilt.StatusDisabled,
}

func TestByName(t *testing.T) {
	for _, name := range Names {
		th, ok := ByName(name)
		if !ok || th.Name != name {
			t.Fatalf("ByName(%q) = %q, %v", name, th.Name, ok)
		}
		for _, s := range allStatuses {
			if _, ok := th.Status[s]; !ok {
				t.Errorf("%s: no style for %v", name, s)
			}
		}
	}
	for _, name := range []string{"nope", "", "Tokyo-Night"} {
		if _, ok := ByName(name); ok {
			t.Errorf("ByName(%q) ok, want false", name)
		}
	}
}

// terminal colours only status, errors and warnings.
func TestTerminalColoursOnlyStatus(t *testing.T) {
	th, _ := ByName("terminal")
	for i, s := range []lipgloss.Style{th.Border, th.BorderFocused, th.Title, th.Dim, th.Selected, th.Match} {
		if s.GetForeground() != (lipgloss.NoColor{}) || s.GetBackground() != (lipgloss.NoColor{}) {
			t.Errorf("terminal style %d sets a colour", i)
		}
	}
	for _, s := range []tilt.Status{tilt.StatusHealthy, tilt.StatusUnhealthy, tilt.StatusPending, tilt.StatusBuilding} {
		if th.Status[s].GetForeground() == (lipgloss.NoColor{}) {
			t.Errorf("terminal status %v has no colour", s)
		}
	}
}

func TestTokyoNightUsesItsPalette(t *testing.T) {
	th, _ := ByName("tokyo-night")
	if got, want := th.Status[tilt.StatusHealthy].GetForeground(), lipgloss.Color("#9ece6a"); got != want {
		t.Errorf("healthy = %v, want %v", got, want)
	}
	if got, want := th.Error.GetForeground(), lipgloss.Color("#f7768e"); got != want {
		t.Errorf("error = %v, want %v", got, want)
	}
}

func TestMonoHasNoColour(t *testing.T) {
	th, _ := ByName("mono")
	styles := []lipgloss.Style{th.Border, th.BorderFocused, th.Title, th.Dim, th.Selected, th.Match, th.Error, th.Warning}
	for _, s := range allStatuses {
		styles = append(styles, th.Status[s])
	}
	for i, s := range styles {
		out := s.Render("x")
		// SGR colour parameters: 30-37, 38, 39, 40-47, 48, 49, 90-97, 100-107.
		if regexp.MustCompile(`\x1b\[[0-9;]*(3[0-9]|4[0-9]|9[0-7]|10[0-7])[;m]`).MatchString(out) {
			t.Errorf("mono style %d emits colour: %q", i, out)
		}
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		noColor, name, want string
		warn                bool
	}{
		{"", "", "default", false},
		{"", "mono", "mono", false},
		{"", "default", "default", false},
		{"", "tokyo-night", "tokyo-night", false},
		{"", "terminal", "terminal", false},
		{"", "nope", "default", true},
		{"1", "default", "mono", false},
		{"1", "tokyo-night", "mono", false},
		{"1", "nope", "mono", false},
	}
	for _, tt := range tests {
		t.Run(tt.noColor+"/"+tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			th, warn := Select(tt.name)
			if th.Name != tt.want || (warn != "") != tt.warn {
				t.Fatalf("Select(%q) = %q, %q", tt.name, th.Name, warn)
			}
			if tt.warn && !strings.Contains(warn, `"nope"`) {
				t.Fatalf("warning %q does not name the theme", warn)
			}
		})
	}
}
