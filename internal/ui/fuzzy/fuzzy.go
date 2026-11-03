// Package fuzzy ranks names against a typed query for the resource
// picker. It ignores case.
package fuzzy

import (
	"sort"
	"strings"
)

// Tiers order matches. A lower tier always ranks first.
const (
	TierExact = iota
	TierPrefix
	TierFuzzy
)

// Match is one item that matched. Index points into the items passed to
// Rank. Gaps counts the skipped characters between the first and last
// matched ones, so a substring has none.
type Match struct {
	Index int
	Tier  int
	Gaps  int
}

// Score matches query against s. Every query character must appear in s
// in order. ok is false when one doesn't.
func Score(query, s string) (m Match, ok bool) {
	q, t := []rune(strings.ToLower(query)), []rune(strings.ToLower(s))
	switch {
	case string(q) == string(t):
		return Match{Tier: TierExact}, true
	case strings.HasPrefix(string(t), string(q)):
		return Match{Tier: TierPrefix}, true
	}
	// Prefer the tightest window: a contiguous run beats a scattered one.
	best := -1
	for start := range t {
		if len(q) == 0 || t[start] != q[0] {
			continue
		}
		qi, end := 0, start
		for i := start; i < len(t) && qi < len(q); i++ {
			if t[i] == q[qi] {
				qi, end = qi+1, i
			}
		}
		if qi < len(q) {
			break
		}
		if gaps := end - start + 1 - len(q); best < 0 || gaps < best {
			best = gaps
		}
	}
	if best < 0 {
		return Match{}, false
	}
	return Match{Tier: TierFuzzy, Gaps: best}, true
}

// Rank returns the items that match query, best first: exact, then
// prefix, then other fuzzy matches by fewest gaps. Ties keep the order of
// items. An empty query matches every item in order.
func Rank(query string, items []string) []Match {
	out := make([]Match, 0, len(items))
	for i, s := range items {
		if m, ok := Score(query, s); ok {
			m.Index = i
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Tier != out[b].Tier {
			return out[a].Tier < out[b].Tier
		}
		return out[a].Gaps < out[b].Gaps
	})
	return out
}
