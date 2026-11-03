// Package theme defines the colour schemes.
package theme

import (
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
func ByName(name string) (t Theme, ok bool) { panic("T0 stub") }

// Select returns the theme to use. A non-empty NO_COLOR environment
// variable forces "mono". An empty name means "default". An unknown name
// falls back to "default" and returns a warning for the user.
func Select(name string) (t Theme, warning string) { panic("T0 stub") }
