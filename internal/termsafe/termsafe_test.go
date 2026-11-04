package termsafe

import (
	"fmt"
	"strings"
	"testing"
	"time"
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
		{"DCS with non-ASCII payload removed", "a\x1bPé\x1b\\b", "ab"},
		{"unterminated APC with 0x9C rune removed", "\x1b_✛31mX", ""},
		{"OSC with CJK payload removed", "a\x1b]0;日本\x07b", "ab"},
		{"PM with non-ASCII payload removed", "a\x1b^ü\x1b\\b", "ab"},
		{"SOS with non-ASCII payload removed", "a\x1bXü\x1b\\b", "ab"},
		{"text after a removed string kept", "\x1b]0;✛\x07x\x1b[1my", "xy"},
		{"C1 OSC payload removed (BEL)", "a\u009d0;x\x07b", "ab"},
		{"C1 OSC payload removed (C1 ST)", "a\u009d0;x\u009cb", "ab"},
		{"C1 DCS payload removed", "a\u0090q\x1b\\b", "ab"},
		{"C1 SOS payload removed", "a\u0098q\x1b\\b", "ab"},
		{"C1 PM payload removed", "a\u009eq\u009cb", "ab"},
		{"C1 APC payload removed", "a\u009fq\u009cb", "ab"},
		{"unterminated C1 OSC removed to end", "a\u009d0;spoof", "a"},
		{"C1 DCS not ended by BEL", "a\u0090q\x07b\x1b\\c", "ac"},
		{"C1 introducer inside an OSC is payload", "a\x1b]\u0090\x1b\\b", "ab"},
		{"joined C1 introducer after a removed string", "a\xc2\u009dP1\x07\x9d0;PAYLOAD\x07b", "ab"},
		{"joined C1 introducer runs to end", "\xc2\xc2\x9dx\x07\x9dspoof", ""},
		{"ESC joined to a string after a C1 removal", "\x1b\u009dx\a]A\x1bPq\a\u009dzz\x1b\\b", "b"},
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
		{"OSC with 0x9C inside a rune leaks nothing", "\x1b]0;✛31mX", ""},
		{"APC with 0x9C inside a rune leaks nothing", "\x1b_✛31mX", ""},
		{"C2 and 9B across a dropped OSC do not join", "\xc2\x1b]0;\x9b31m", "�"},
		{"stranded 0x9D does not start a C1 OSC", "\x1b]0;✝52;c;ZXZpbA==\aX", "X"},
		{"stranded 0x9C dropped", "\x1b]0;✜Xb", ""},
		{"OSC with 0x9D in a rune removed whole", "a\x1b]0;᜝52;c;x\x07b", "ab"},
		{"unterminated OSC with 0x9B in a rune runs to end", "a\x1b]0;᜛2Jb", "a"},
		{"DCS with 0x9B in a rune removed whole", "a\x1bPq᜛2J\x1b\\b", "ab"},
		{"OSC with wide runes removed whole", "a\x1b]0;日本\x07b", "ab"},
		{"OSC without terminator swallows a later SGR", "a\x1b]0;本\x1b[31mb", "a"},
		{"DCS with a two-byte rune removed whole", "a\x1bPé\x1b\\b", "ab"},
		{"APC not ended by BEL", "a\x1b_payload\x07b", "a"},
		{"OSC with 9C 9B in a four-byte rune leaks nothing", "a\x1b]0;\U0001C6DBb", "a"},
		{"PM removed whole", "a\x1b^é\x1b\\b", "ab"},
		{"SOS removed whole", "a\x1bXé\x1b\\b", "ab"},
		{"C1 OSC payload removed (BEL)", "a\u009d0;x\x07b", "ab"},
		{"C1 OSC payload removed (C1 ST)", "a\u009d0;x\u009cb", "ab"},
		{"C1 DCS payload removed", "a\u0090q\x1b\\b", "ab"},
		{"C1 SOS payload removed", "a\u0098q\x1b\\b", "ab"},
		{"C1 PM payload removed", "a\u009eq\u009cb", "ab"},
		{"C1 APC payload removed", "a\u009fq\u009cb", "ab"},
		{"unterminated C1 OSC removed to end", "a\u009d0;spoof", "a"},
		{"C1 DCS not ended by BEL", "a\u0090q\x07b\x1b\\c", "ac"},
		{"C1 introducer inside an OSC is payload", "a\x1b]\u0090\x1b\\b", "ab"},
		{"ESC joined to a string after a C1 removal", "\x1b\u009dx\a]A\x1bPq\a\u009dzz\x1b\\b", "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogLine(tt.in)
			if got != tt.want {
				t.Errorf("LogLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("LogLine(%q) = %q is not valid UTF-8", tt.in, got)
			}
			if i := strayC1(got); i >= 0 {
				t.Errorf("LogLine(%q) = %q has stray C1 byte %#x at %d", tt.in, got, got[i], i)
			}
			if !onlySGR(got) {
				t.Errorf("LogLine(%q) = %q leaks a non-SGR control", tt.in, got)
			}
		})
	}
}

func TestLogLineStrandedC1OSC(t *testing.T) {
	in := "\x1b]0;✝52;c;ZXZpbA==\aX"
	got := LogLine(in)
	if i := strings.IndexByte(got, '\a'); i >= 0 {
		t.Errorf("LogLine(%q) = %q has BEL at %d", in, got, i)
	}
	if i := strings.IndexByte(got, 0x9d); i >= 0 {
		t.Errorf("LogLine(%q) = %q has byte 0x9d at %d", in, got, i)
	}
}

// byteJoins returns an input that needs k passes of dropC1Strings that
// remove bytes. Each pass removes one string and joins the next lone C2
// with the next raw 9D into a new U+009D. ToValidUTF8 turns both bytes
// into U+FFFD, so this nesting never reaches dropC1Strings in LogLine.
func byteJoins(k int) string {
	return "a" + strings.Repeat("\xc2", k-1) + "\u009dP\a" + strings.Repeat("\x9dx\a", k-1) + "b"
}

// escJoins returns valid UTF-8 that needs k passes of dropC1Strings that
// remove bytes. Each pass removes one C1 string and joins the ESC before
// it with the "]" after it. The new OSC hides the ESC P that used to hide
// the next C1 string, so the next pass finds that string.
func escJoins(k int) string {
	var b strings.Builder
	b.WriteString("\x1b\u009dx\a")
	for i := 1; i < k; i++ {
		b.WriteString("]A\x1bPq\a")
		if i < k-1 {
			b.WriteString("\x1b")
		}
		b.WriteString("\u009dz\x1b\\")
	}
	b.WriteString("b")
	return b.String()
}

// dropC1StringsFixpoint reruns dropC1Strings with no bound. It returns
// the result and the number of passes that removed bytes.
func dropC1StringsFixpoint(s string) (string, int) {
	for n := 0; ; n++ {
		t := dropC1Strings(s)
		if len(t) == len(s) {
			return s, n
		}
		s = t
	}
}

func TestNestedJoinsFailClosed(t *testing.T) {
	for _, k := range []int{7, 9} {
		for name, in := range map[string]string{"bytes": byteJoins(k), "ESC": escJoins(k)} {
			if _, n := dropC1StringsFixpoint(in); n != k {
				t.Fatalf("%s k=%d: input needs %d passes, want %d", name, k, n, k)
			}
		}
	}

	t.Run("k=7 matches the unbounded loop", func(t *testing.T) {
		in := byteJoins(7)
		stable, _ := dropC1StringsFixpoint(in)
		if got, want := Text(in), Text(stable); got != want || got != "ab" {
			t.Errorf("Text(byteJoins(7)) = %q, want %q", got, want)
		}
		in = escJoins(7)
		stable, _ = dropC1StringsFixpoint(in)
		if got, want := Text(in), Text(stable); got != want || got != "b" {
			t.Errorf("Text(escJoins(7)) = %q, want %q", got, want)
		}
		if got, want := LogLine(in), LogLine(stable); got != want || got != "b" {
			t.Errorf("LogLine(escJoins(7)) = %q, want %q", got, want)
		}
	})

	for _, k := range []int{9, 100000} {
		t.Run(fmt.Sprintf("k=%d fails closed", k), func(t *testing.T) {
			calls := []struct {
				name string
				f    func(string) string
				in   string
			}{
				{"Text(byteJoins)", Text, byteJoins(k)},
				{"Text(escJoins)", Text, escJoins(k)},
				{"LogLine(escJoins)", LogLine, escJoins(k)},
			}
			for _, c := range calls {
				start := time.Now()
				got := c.f(c.in)
				if d := time.Since(start); d > time.Second {
					t.Errorf("%s took %v, want under 1s", c.name, d)
				}
				if got != "�" {
					t.Errorf("%s = %q, want %q", c.name, got, "�")
				}
			}
		})
	}
}

func BenchmarkTextNestedJoins(b *testing.B) {
	for _, k := range []int{1000, 10000} {
		in := byteJoins(k)
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			for b.Loop() {
				Text(in)
			}
		})
	}
}

// strayC1 returns the index of the first byte in 0x80 to 0x9F that is
// not part of a valid multi-byte rune, or -1 if there is none.
func strayC1(s string) int {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 && s[i] >= 0x80 && s[i] <= 0x9f {
			return i
		}
		i += size
	}
	return -1
}

// onlySGR reports whether s is valid UTF-8, every ESC in s starts a
// complete SGR sequence and s holds no other control bytes.
func onlySGR(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
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
	"\x1b]0;✛31m", "\x1b_✛", "\x1b]0;✝52;c;ZXZpbA==\aX", "\x1b]0;✜Xb",
	"a\x1b]0;᜝52;c;x\x07b", "a\x1b]0;᜛2Jb", "a\x1bPq᜛2J\x1b\\b", "a\x1b]0;日本\x07b",
	"a\x1b]0;本\x1b[31mb", "a\x1bPé\x1b\\b", "a\x1b_payload\x07b", "a\x1b]0;\U0001C6DBb",
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
	f.Add("a\x1bPé\x1b\\b")
	f.Add("\x1b_✛31mX")
	f.Add("a\x1b]0;日本\x07b")
	f.Add("a\x1b^ü\x1b\\b")
	f.Add("a\x1bXü\x1b\\b")
	f.Add("\x1b]0;✛\x07x\x1b[1my")
	f.Fuzz(func(t *testing.T, in string) {
		out := Text(in)
		for _, r := range out {
			if r == 0x1b || unicode.IsControl(r) {
				t.Fatalf("Text(%q) = %q contains control %U", in, out, r)
			}
		}
	})
}

// FuzzTextDropsStrings wraps an arbitrary payload in each string
// introducer and checks that only the text around it survives.
func FuzzTextDropsStrings(f *testing.F) {
	f.Add("é")
	f.Add("✛31m")
	f.Add("日本")
	f.Fuzz(func(t *testing.T, payload string) {
		if strings.ContainsAny(payload, "\x1b\x07") {
			return
		}
		for _, intro := range []string{"]", "P", "_", "^", "X"} {
			in := "a\x1b" + intro + payload + "\x1b\\b"
			if got := Text(in); got != "ab" {
				t.Fatalf("Text(%q) = %q, want %q", in, got, "ab")
			}
		}
	})
}

// FuzzC1StringsDropped wraps an arbitrary payload in each C1 string
// introducer and checks that only the text around it survives.
func FuzzC1StringsDropped(f *testing.F) {
	f.Add("0;title")
	f.Add("日本")
	f.Fuzz(func(t *testing.T, payload string) {
		if strings.ContainsAny(payload, "\x1b\x07") || strings.Contains(payload, "\u009c") {
			return
		}
		for _, intro := range []string{"\u0090", "\u0098", "\u009d", "\u009e", "\u009f"} {
			in := "a" + intro + payload + "\u009cb"
			if got := Text(in); got != "ab" {
				t.Fatalf("Text(%q) = %q, want %q", in, got, "ab")
			}
			if got := LogLine(in); got != "ab" {
				t.Fatalf("LogLine(%q) = %q, want %q", in, got, "ab")
			}
		}
	})
}
