package logview

import (
	"slices"
	"strings"
	"testing"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		name string
		in   string
		w    int
		want []string
	}{
		{"fits", "hello", 10, []string{"hello"}},
		{"empty", "", 10, []string{""}},
		{"ascii", strings.Repeat("a", 25), 10, []string{"aaaaaaaaaa", "↳ aaaaaaaa", "↳ aaaaaaa"}},
		{"wide chars never split", "日本語テキスト", 5, []string{"日本", "↳ 語", "↳ テ", "↳ キ", "↳ ス", "↳ ト"}},
		{"SGR carried to next row", "\x1b[31mabcdef", 4, []string{"\x1b[31mabcd", "↳ \x1b[31mef"}},
		{"reset clears carried SGR", "\x1b[31mab\x1b[0mcdef", 4, []string{"\x1b[31mab\x1b[0mcd", "↳ ef"}},
		{"combining marks stay attached", strings.Repeat("é", 5), 4, []string{strings.Repeat("é", 4), "↳ é"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrap(tt.in, tt.w); !slices.Equal(got, tt.want) {
				t.Fatalf("wrap(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
			}
		})
	}
}
