package termsafe

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// --- Contract assertions ---

var (
	_ func(string) string = Text
	_ func(string) string = LogLine
)

// --- Behavior tests ---

func TestText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text unchanged", "api-server", "api-server"},
		{"SGR color stripped", "\x1b[31mred\x1b[0m", "red"},
		{"OSC 52 clipboard write stripped", "x\x1b]52;c;ZXZpbA==\x07y", "xy"},
		{"OSC title change stripped", "\x1b]0;pwned\x1b\\ok", "ok"},
		{"newline and tab become spaces", "a\nb\tc", "a b c"},
		{"two-byte ESC sequence stripped", "a\x1bb", "a"},
		{"trailing lone ESC removed", "a\x1b", "a"},
		{"BEL replaced", "a\x07b", "a�b"},
		{"C1 CSI replaced", "a\u009b31mb", "a�31mb"},
		{"unicode kept", "résumé ✓", "résumé ✓"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Text(tt.in)
			if got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.ContainsRune(got, '\x1b') {
				t.Errorf("Text(%q) still contains ESC", tt.in)
			}
		})
	}
}

func TestLogLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text unchanged", "hello world", "hello world"},
		{"SGR kept", "\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"},
		{"SGR with colon params kept", "\x1b[38:5:196mx\x1b[m", "\x1b[38:5:196mx\x1b[m"},
		{"OSC 52 removed", "a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		{"OSC title removed", "\x1b]0;pwned\x1b\\ok", "ok"},
		{"cursor move removed", "a\x1b[2Jb\x1b[10;5Hc", "abc"},
		{"private-mode CSI ending in m removed", "a\x1b[?25mb", "ab"},
		{"two-byte ESC removed", "a\x1bcb", "ab"},
		{"incomplete trailing CSI removed", "abc\x1b[31", "abc"},
		{"lone trailing ESC removed", "abc\x1b", "abc"},
		{"C1 CSI replaced, not parsed", "a\u009b31mb", "a�31mb"},
		{"BEL replaced", "a\x07b", "a�b"},
		{"DEL replaced", "a\x7fb", "a�b"},
		{"CR removed", "line\r", "line"},
		{"tab to next 8 columns", "ab\tc", "ab      c"},
		{"tab ignores SGR width", "\x1b[1mab\x1b[0m\tc", "\x1b[1mab\x1b[0m      c"},
		{"tab after wide chars", "日本\tx", "日本    x"},
		{"invalid UTF-8 replaced", "a\xffb", "a�b"},
		{"unicode kept", "résumé ✓ 日本", "résumé ✓ 日本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LogLine(tt.in); got != tt.want {
				t.Errorf("LogLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// onlySGR reports whether every ESC in s starts a complete SGR sequence
// and s holds no other control bytes.
func onlySGR(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			j := i + 2
			if j > len(s) || s[i+1] != '[' {
				return false
			}
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';' || s[j] == ':') {
				j++
			}
			if j >= len(s) || s[j] != 'm' {
				return false
			}
			i = j
			continue
		}
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	for _, r := range s {
		if r >= 0x80 && r <= 0x9f {
			return false
		}
	}
	return true
}

var fuzzSeeds = []string{
	"plain", "\x1b[31mred\x1b[0m", "\x1b]52;c;ZXZpbA==\x07", "\x1b]0;t\x1b\\",
	"\u009b31m", "a\tb\r", "\x1b[?25m", "\x1b[", "\x1bP1$r\x1b\\", "\xff\xfe",
}

func FuzzLogLine(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		in = strings.ReplaceAll(in, "\n", "")
		out := LogLine(in)
		if !onlySGR(out) {
			t.Fatalf("LogLine(%q) = %q leaks a non-SGR control", in, out)
		}
		if !utf8.ValidString(out) {
			t.Fatalf("LogLine(%q) = %q is not valid UTF-8", in, out)
		}
	})
}

func FuzzText(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Text(in)
		for _, r := range out {
			if r == 0x1b || unicode.IsControl(r) {
				t.Fatalf("Text(%q) = %q contains control %U", in, out, r)
			}
		}
	})
}
