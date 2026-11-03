package termsafe

import (
	"strings"
	"testing"
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
