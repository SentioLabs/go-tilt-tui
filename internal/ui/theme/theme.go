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

// ByName returns the named theme. ok is false for unknown names.
func ByName(name string) (t Theme, ok bool) {
	switch name {
	case "default":
		return defaultTheme(), true
	case "tokyo-night":
		return tokyoNightTheme(), true
	case "terminal":
		return terminalTheme(), true
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

// tokyoNightTheme uses the Tokyo Night palette in truecolor. It sets no
// background on plain text, so the terminal's background shows.
func tokyoNightTheme() Theme {
	fg := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	const (
		comment = "#565f89"
		text    = "#c0caf5"
		blue    = "#7aa2f7"
		green   = "#9ece6a"
		red     = "#f7768e"
		yellow  = "#e0af68"
		border  = "#3b4261"
		selBg   = "#283457"
		bg      = "#1a1b26"
	)
	return Theme{
		Name: "tokyo-night",
		Status: map[tilt.Status]lipgloss.Style{
			tilt.StatusNone:      fg(comment),
			tilt.StatusPending:   fg(yellow),
			tilt.StatusBuilding:  fg(blue),
			tilt.StatusHealthy:   fg(green),
			tilt.StatusUnhealthy: fg(red),
			tilt.StatusDisabled:  fg(comment),
		},
		Border:        fg(border),
		BorderFocused: fg(blue),
		Title:         fg(text).Bold(true),
		Dim:           fg(comment),
		Selected:      fg(text).Background(lipgloss.Color(selBg)),
		Match:         fg(bg).Background(lipgloss.Color(yellow)),
		Error:         fg(red),
		Warning:       fg(yellow),
	}
}

// terminalTheme keeps the terminal's own colours. Only status dots,
// errors and warnings get ANSI colour. Everything else uses attributes.
func terminalTheme() Theme {
	plain := lipgloss.NewStyle()
	fg := func(c color.Color) lipgloss.Style { return plain.Foreground(c) }
	return Theme{
		Name: "terminal",
		Status: map[tilt.Status]lipgloss.Style{
			tilt.StatusNone:      plain.Faint(true),
			tilt.StatusPending:   fg(lipgloss.Yellow),
			tilt.StatusBuilding:  fg(lipgloss.Blue),
			tilt.StatusHealthy:   fg(lipgloss.Green),
			tilt.StatusUnhealthy: fg(lipgloss.Red),
			tilt.StatusDisabled:  plain.Faint(true),
		},
		Border:        plain.Faint(true),
		BorderFocused: plain.Bold(true),
		Title:         plain.Bold(true),
		Dim:           plain.Faint(true),
		Selected:      plain.Reverse(true),
		Match:         plain.Underline(true).Bold(true),
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
