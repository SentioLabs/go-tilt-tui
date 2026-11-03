// Package theme defines the colour schemes.
package theme

import (
	"fmt"
	"image/color"
	"os"

	"charm.land/lipgloss/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

type Theme struct {
	Name          string
	Status        map[tilt.Status]lipgloss.Style
	Border        lipgloss.Style
	BorderFocused lipgloss.Style
	Title         lipgloss.Style
	Dim           lipgloss.Style
	Selected      lipgloss.Style
	Match         lipgloss.Style // search highlight
	Error         lipgloss.Style
	Warning       lipgloss.Style // the "!" build-warning marker and count
}

// Names lists the built-in themes in display order.
var Names = []string{"default", "tokyo-night", "terminal", "mono"}

// ByName returns the named theme. ok is false for unknown names and for
// names in Names that aren't built yet.
func ByName(name string) (t Theme, ok bool) {
	switch name {
	case "default":
		return defaultTheme(), true
	case "mono":
		return monoTheme(), true
	}
	return Theme{}, false
}

// Select returns the theme to use. A non-empty NO_COLOR environment
// variable forces "mono". An empty name means "default". An unknown name
// falls back to "default" and returns a warning for the user.
func Select(name string) (t Theme, warning string) {
	if os.Getenv("NO_COLOR") != "" {
		return monoTheme(), ""
	}
	if name == "" {
		return defaultTheme(), ""
	}
	if t, ok := ByName(name); ok {
		return t, ""
	}
	return defaultTheme(), fmt.Sprintf("unknown theme %q, using default", name)
}

// defaultTheme uses the 16 ANSI colours so it follows the terminal palette.
func defaultTheme() Theme {
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	return Theme{
		Name: "default",
		Status: map[tilt.Status]lipgloss.Style{
			tilt.StatusNone:      fg(lipgloss.BrightBlack),
			tilt.StatusPending:   fg(lipgloss.Yellow),
			tilt.StatusBuilding:  fg(lipgloss.Blue),
			tilt.StatusHealthy:   fg(lipgloss.Green),
			tilt.StatusUnhealthy: fg(lipgloss.Red),
			tilt.StatusDisabled:  fg(lipgloss.BrightBlack),
		},
		Border:        fg(lipgloss.BrightBlack),
		BorderFocused: fg(lipgloss.Blue),
		Title:         lipgloss.NewStyle().Bold(true),
		Dim:           lipgloss.NewStyle().Faint(true),
		Selected:      lipgloss.NewStyle().Reverse(true),
		Match:         lipgloss.NewStyle().Foreground(lipgloss.Black).Background(lipgloss.Yellow),
		Error:         fg(lipgloss.Red),
		Warning:       fg(lipgloss.Yellow),
	}
}

// monoTheme uses no colour. Statuses differ only by text attributes.
func monoTheme() Theme {
	plain := lipgloss.NewStyle()
	return Theme{
		Name: "mono",
		Status: map[tilt.Status]lipgloss.Style{
			tilt.StatusNone:      plain.Faint(true),
			tilt.StatusPending:   plain,
			tilt.StatusBuilding:  plain,
			tilt.StatusHealthy:   plain,
			tilt.StatusUnhealthy: plain.Bold(true),
			tilt.StatusDisabled:  plain.Faint(true),
		},
		Border:        plain,
		BorderFocused: plain.Bold(true),
		Title:         plain.Bold(true),
		Dim:           plain.Faint(true),
		Selected:      plain.Reverse(true),
		Match:         plain.Underline(true).Bold(true),
		Error:         plain.Bold(true),
		Warning:       plain.Bold(true),
	}
}
