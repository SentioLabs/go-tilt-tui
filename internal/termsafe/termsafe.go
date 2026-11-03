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
func LogLine(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r >= 0x80 && r <= 0x9f {
			return unicode.ReplacementChar
		}
		return r
	}, s)

	var b strings.Builder
	b.Grow(len(s))
	col := 0
	state := byte(ansi.NormalState)
	for len(s) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(s, state, nil)
		state = newState
		s = s[n:]
		switch {
		case seq == "\t":
			pad := 8 - col%8
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		case seq == "\r":
			// dropped
		case strings.HasPrefix(seq, "\x1b"):
			if isSGR(seq) {
				b.WriteString(seq)
			}
		case len(seq) == 1 && (seq[0] < 0x20 || seq[0] == 0x7f):
			b.WriteRune(unicode.ReplacementChar)
			col++
		default:
			b.WriteString(seq)
			col += width
		}
	}
	return b.String()
}

// isSGR reports whether seq is a complete Select Graphic Rendition
// sequence: ESC [ params m, with params made only of digits, ';' and ':'.
func isSGR(seq string) bool {
	if len(seq) < 3 || !strings.HasPrefix(seq, "\x1b[") || seq[len(seq)-1] != 'm' {
		return false
	}
	for i := 2; i < len(seq)-1; i++ {
		c := seq[i]
		if (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}
