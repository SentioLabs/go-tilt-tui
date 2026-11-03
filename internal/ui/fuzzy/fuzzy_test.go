package fuzzy

import (
	"slices"
	"testing"
)

// --- Contract assertions ---

var (
	_ func(string, string) (Match, bool) = Score
	_ func(string, []string) []Match     = Rank
)

// --- Behavior tests ---

func names(items []string, ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = items[m.Index]
	}
	return out
}

func TestScore(t *testing.T) {
	tests := []struct {
		q, s string
		want Match
		ok   bool
	}{
		{"api", "api", Match{Tier: TierExact}, true},
		{"API", "api", Match{Tier: TierExact}, true},
		{"api", "api-gateway", Match{Tier: TierPrefix}, true},
		{"gate", "api-gateway", Match{Tier: TierFuzzy}, true},
		{"agw", "api-gateway", Match{Tier: TierFuzzy, Gaps: 6}, true},
		{"aiy", "a-i-api-y", Match{Tier: TierFuzzy, Gaps: 2}, true},
		{"xyz", "api", Match{}, false},
		{"ipa", "api", Match{}, false},
		{"", "web", Match{Tier: TierPrefix}, true},
		{"日本", "サービス日本", Match{Tier: TierFuzzy}, true},
	}
	for _, tt := range tests {
		got, ok := Score(tt.q, tt.s)
		if ok != tt.ok || got != tt.want {
			t.Errorf("Score(%q, %q) = %+v %v, want %+v %v", tt.q, tt.s, got, ok, tt.want, tt.ok)
		}
	}
}

// The ranking bug in tilt-tui's TODO: a fuzzy match listed ahead of the
// exact name.
func TestRankExactThenPrefixThenFuzzy(t *testing.T) {
	items := []string{"web-api-proxy", "api-gateway", "a-p-i", "api", "worker", "my-api"}
	got := names(items, Rank("api", items))
	want := []string{"api", "api-gateway", "web-api-proxy", "my-api", "a-p-i"}
	if !slices.Equal(got, want) {
		t.Fatalf("Rank(api) = %q, want %q", got, want)
	}
}

func TestRankEmptyQueryKeepsOrder(t *testing.T) {
	items := []string{"web", "api", "db"}
	if got := names(items, Rank("", items)); !slices.Equal(got, items) {
		t.Fatalf("Rank(\"\") = %q", got)
	}
	if got := Rank("zzz", items); len(got) != 0 {
		t.Fatalf("Rank(zzz) = %v", got)
	}
}
