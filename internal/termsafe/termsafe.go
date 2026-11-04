// Package termsafe is the only code that decides which bytes from Tilt
// reach the terminal.
package termsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Text returns s with every escape sequence removed, tabs and newlines
// turned into spaces, and other C0/C1 control characters replaced with
// U+FFFD. Strings opened by a C1 introducer rune are removed with their
// payload. Use it for names, errors, labels, URLs and paths.
func Text(s string) string {
	s = dropC1Strings(s)
	var b strings.Builder
	b.Grow(len(s))
	for len(s) > 0 {
		if n := stringLen(s); n > 0 {
			s = s[n:]
			continue
		}
		// Cut s before its next ESC, so no chunk holds the start of a
		// string. ansi.Strip handles CSI and other short sequences.
		end := len(s)
		if i := strings.IndexByte(s[1:], 0x1b); i >= 0 {
			end = i + 1
		}
		b.WriteString(ansi.Strip(s[:end]))
		s = s[end:]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return unicode.ReplacementChar
		default:
			return r
		}
	}, b.String())
}

// LogLine returns s with SGR (colour and style) sequences kept and every
// other escape sequence removed. Tabs expand to spaces; other control
// characters become U+FFFD. Strings opened by a C1 introducer rune are
// removed with their payload. s must be a single line without "\n".
func LogLine(s string) string {
	s = replaceC1(dropC1Strings(strings.ToValidUTF8(s, "�")))

	var b strings.Builder
	b.Grow(len(s))
	col := 0
	state := byte(ansi.NormalState)
	for len(s) > 0 {
		if n := stringLen(s); n > 0 {
			s = s[n:]
			state = ansi.NormalState
			continue
		}
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
		case !isSafeText(seq):
			// Defence in depth: a continuation byte stranded by a split
			// rune (see below) can decode as a C1 control, and 0x9D starts
			// a C1 OSC that swallows raw bytes. Drop the whole seq.
			b.WriteRune(unicode.ReplacementChar)
			col++
		default:
			b.WriteString(seq)
			col += width
		}
	}
	// Defence in depth. Inside a string the decoder treats a 0x9C
	// continuation byte (as in U+271B, E2 9C 9B) as an 8-bit String
	// Terminator. The string then ends mid-rune and the stranded 0x9B
	// would reach the output raw. stringLen keeps strings away from the
	// decoder, and this pass repairs the output if a split ever happens.
	// It also replaces C1 runes in case stray bytes from both sides of a
	// dropped sequence joined into one. Kept SGR sequences are ASCII and
	// pass through unchanged.
	return replaceC1(strings.ToValidUTF8(b.String(), "�"))
}

// replaceC1 returns s with every C1 control rune (U+0080 to U+009F)
// replaced by U+FFFD.
func replaceC1(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x80 && r <= 0x9f {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}

// stringLen returns the length of the OSC, DCS, APC, PM or SOS string at
// the start of s, or 0 if s doesn't start with one. A string ends after
// the first 7-bit ST (ESC \). A BEL also ends an OSC. Without a
// terminator the string runs to the end of s, so a lone ESC inside it is
// payload and doesn't end it. Bytes >= 0x80 are payload too, and 0x9C
// never ends a string because it may be part of a rune.
func stringLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b {
		return 0
	}
	switch s[1] {
	case ']', 'P', '_', '^', 'X':
	default:
		return 0
	}
	for i := 2; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		if s[1] == ']' && s[i] == 0x07 {
			return i + 1
		}
	}
	return len(s)
}

// dropC1Strings returns s with every C1 string removed. A C1 string
// starts at a UTF-8 encoded DCS, SOS, OSC, PM or APC introducer
// (U+0090, U+0098, U+009D, U+009E, U+009F). It ends after ESC \ or
// U+009C, and an OSC also ends after BEL. Without a terminator it
// runs to the end of s. A 7-bit string is copied through unchanged.
// Tilt text is UTF-8, so these runes are how an 8-bit string introducer
// reaches us.
func dropC1Strings(s string) string {
	if !strings.Contains(s, "\xc2") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		// A C1 introducer inside a 7-bit string is payload. Copy the
		// string whole so the caller drops it at its own terminator.
		if n := stringLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		if s[i] != 0xc2 || i+1 >= len(s) || !isC1StringIntro(s[i+1]) {
			b.WriteByte(s[i])
			i++
			continue
		}
		osc := s[i+1] == 0x9d
		j := i + 2
		for ; j < len(s); j++ {
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				j += 2
				break
			}
			if s[j] == 0xc2 && j+1 < len(s) && s[j+1] == 0x9c {
				j += 2
				break
			}
			if osc && s[j] == 0x07 {
				j++
				break
			}
		}
		i = j
	}
	return b.String()
}

// isC1StringIntro reports whether c is the second byte of a UTF-8
// encoded DCS, SOS, OSC, PM or APC rune.
func isC1StringIntro(c byte) bool {
	switch c {
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return true
	}
	return false
}

// isSafeText reports whether seq is valid UTF-8 that starts with no
// continuation byte and holds no C0, DEL or C1 control runes.
func isSafeText(seq string) bool {
	if seq[0] >= 0x80 && seq[0] <= 0xbf || !utf8.ValidString(seq) {
		return false
	}
	for _, r := range seq {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
			return false
		}
	}
	return true
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
