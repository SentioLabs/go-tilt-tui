// Package termsafe is the only code that decides which bytes from Tilt
// reach the terminal.
package termsafe

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Text returns s with every escape sequence removed, tabs and newlines
// turned into spaces, and other C0/C1 control characters replaced with
// U+FFFD. Use it for names, errors, labels, URLs and paths.
func Text(s string) string {
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

// LogLine returns s with SGR (colour and style) sequences kept and every
// other escape sequence removed. Tabs expand to spaces; other control
// characters become U+FFFD. s must be a single line without "\n".
func LogLine(s string) string { panic("T0 stub") }
