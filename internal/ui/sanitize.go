package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// safe makes a string from Tilt fit to print on the terminal.
//
// Resource names, build errors and error messages come from Tiltfiles and
// command output, so they can carry escape sequences. Printed raw, those can
// retitle the terminal, write the clipboard (OSC 52) or redraw the screen.
// safe removes every escape sequence, turns tabs and newlines into spaces,
// and replaces any other C0/C1 control character with U+FFFD.
func safe(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return unicode.ReplacementChar
		default:
			return r
		}
	}, s)
}
