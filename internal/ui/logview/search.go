package logview

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
)

// scanBatch is how many lines one store read returns during a search.
const scanBatch = 4096

// matcher tests log text against a query. It matches the text with SGR
// removed. fold is set when the query has no capital letter, and then q
// is already lower case.
type matcher struct {
	q     string
	qr    []rune
	fold  bool
	ascii bool // q is pure ASCII
}

func newMatcher(q string) matcher {
	fold := !strings.ContainsFunc(q, unicode.IsUpper)
	if fold {
		q = strings.Map(unicode.ToLower, q)
	}
	return matcher{q: q, qr: []rune(q), fold: fold, ascii: isASCII(q)}
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// match reports whether text holds the query.
func (mt matcher) match(text string) bool {
	if mt.q == "" {
		return false
	}
	if strings.IndexByte(text, '\x1b') >= 0 {
		text = ansi.Strip(text)
	}
	if !mt.fold {
		return strings.Contains(text, mt.q)
	}
	if isASCII(text) {
		// Lower-casing keeps ASCII text ASCII, so it can't hold a
		// query with other characters.
		return mt.ascii && asciiContainsFold(text, mt.q)
	}
	return strings.Contains(strings.Map(unicode.ToLower, text), mt.q)
}

// asciiContainsFold reports whether s holds the lower-case query q,
// ignoring the case of s. Both are pure ASCII.
func asciiContainsFold(s, q string) bool {
	for i := 0; i+len(q) <= len(s); i++ {
		j := 0
		for j < len(q) && lowerASCII(s[i+j]) == q[j] {
			j++
		}
		if j == len(q) {
			return true
		}
	}
	return false
}

func lowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

// span is a match as byte offsets into the text with SGR kept.
type span struct{ start, end int }

// spans returns every match in text, left to right and never
// overlapping. Each span covers whole grapheme clusters, so a highlight
// never splits a wide or combined character.
func (mt matcher) spans(text string) []span {
	var (
		hay        []rune
		gStart     []int
		gEnd       []int
		off        int
		state      = byte(ansi.NormalState)
		rest       = text
		isEscape   = func(s string) bool { return strings.HasPrefix(s, "\x1b") }
		foldedRune = func(r rune) rune {
			if mt.fold {
				return unicode.ToLower(r)
			}
			return r
		}
	)
	// cluster is the hay index of the last printable cluster's first
	// rune, or -1 after an escape. A zero-width piece right after a
	// printable one joins its cluster: the decoder hands out a combining
	// mark after an ASCII letter on its own.
	cluster := -1
	for len(rest) > 0 {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		state = next
		switch {
		case isEscape(seq):
			cluster = -1
		case width == 0 && cluster >= 0:
			for i := cluster; i < len(gEnd); i++ {
				gEnd[i] = off + n
			}
			for _, r := range seq {
				hay = append(hay, foldedRune(r))
				gStart = append(gStart, gStart[cluster])
				gEnd = append(gEnd, off+n)
			}
		default:
			cluster = len(hay)
			for _, r := range seq {
				hay = append(hay, foldedRune(r))
				gStart = append(gStart, off)
				gEnd = append(gEnd, off+n)
			}
		}
		off += n
		rest = rest[n:]
	}
	var out []span
	for i := 0; i+len(mt.qr) <= len(hay); {
		if !runesEqual(hay[i:i+len(mt.qr)], mt.qr) {
			i++
			continue
		}
		s := span{start: gStart[i], end: gEnd[i+len(mt.qr)-1]}
		if n := len(out); n > 0 && s.start <= out[n-1].end {
			out[n-1].end = max(out[n-1].end, s.end)
		} else {
			out = append(out, s)
		}
		i += len(mt.qr)
	}
	return out
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// highlight draws every match in text with the SGR sequence on. SGR
// sequences inside a match are held back, so the highlight stays whole.
// After a match it resets and re-emits the SGR state the line had built
// up, so the line's own colours carry on.
func highlight(text string, spans []span, on string) string {
	if len(spans) == 0 {
		return text
	}
	var (
		b       strings.Builder
		sgr     []string
		off     int
		inMatch bool
		state   = byte(ansi.NormalState)
		rest    = text
	)
	b.Grow(len(text) + len(spans)*(len(on)+8))
	end := func() {
		b.WriteString(sgrReset)
		for _, s := range sgr {
			b.WriteString(s)
		}
		inMatch = false
		spans = spans[1:]
	}
	for len(rest) > 0 {
		if inMatch && off >= spans[0].end {
			end()
		}
		if !inMatch && len(spans) > 0 && off >= spans[0].start {
			b.WriteString(on)
			inMatch = true
		}
		seq, _, n, next := ansi.DecodeSequence(rest, state, nil)
		state = next
		rest = rest[n:]
		off += n
		if strings.HasPrefix(seq, "\x1b") {
			if seq == "\x1b[m" || seq == "\x1b[0m" {
				sgr = sgr[:0]
			} else {
				sgr = append(sgr, seq)
			}
			if inMatch {
				continue
			}
		}
		b.WriteString(seq)
	}
	if inMatch {
		end()
	}
	return b.String()
}

// sgrOpen returns the escape sequence that turns style s on, taken from
// rendering one marker character.
func sgrOpen(render func(...string) string) string {
	out := render("x")
	if i := strings.IndexByte(out, 'x'); i > 0 {
		return out[:i]
	}
	return ""
}

// Parts lists the single-buffer sources whose lines make up src. Search
// reads them one at a time, because merging every buffer by Seq costs
// far more. An All source spans the global logs and every resource in
// resources. Any other source is its own only part.
func Parts(src logstore.Source, resources []string) []logstore.Source {
	if !src.All {
		return []logstore.Source{src}
	}
	parts := make([]logstore.Source, 0, len(resources)+1)
	parts = append(parts, logstore.Source{})
	for _, r := range resources {
		if r != "" {
			parts = append(parts, logstore.Source{Resource: r})
		}
	}
	return parts
}

// SetQuery sets the search query and highlights its matches. An empty
// query ends the search. It forgets the current match.
func (m *Model) SetQuery(q string) {
	if q == m.query {
		return
	}
	m.query = q
	m.mt = newMatcher(q)
	m.match, m.matchPos, m.matchTotal = 0, 0, 0
	clear(m.cache)
}

// Query returns the search query, or "" when no search is active.
func (m *Model) Query() string { return m.query }

// MatchPos returns the current match's place among the matching lines
// and how many lines matched, as counted by the last NextMatch or
// PrevMatch. ok is false when there is no current match.
func (m *Model) MatchPos() (pos, total int, ok bool) {
	return m.matchPos, m.matchTotal, m.match != 0
}

// NextMatch scrolls to the first matching line after the current match.
// With no current match it starts at the top line on screen, which can
// itself match. After the newest match it wraps to the oldest. parts
// comes from Parts. It returns false, and leaves the view alone, when no
// line matches.
func (m *Model) NextMatch(parts []logstore.Source) bool { return m.jump(parts, true) }

// PrevMatch scrolls to the last matching line before the current match,
// or before the top line on screen. It wraps from the oldest match to the
// newest.
func (m *Model) PrevMatch(parts []logstore.Source) bool { return m.jump(parts, false) }

func (m *Model) jump(parts []logstore.Source, forward bool) bool {
	if m.query == "" {
		return false
	}
	m.checkReset()
	// from is the line the walk starts at. A new search may land on the
	// top line itself, so it starts one Seq earlier.
	from, inclusive := m.match, false
	if from == 0 {
		m.normalize()
		from, inclusive = m.top, true
	}
	var (
		seqs        []logstore.Seq
		first, last logstore.Seq
		next, prev  logstore.Seq
	)
	for _, p := range parts {
		var after logstore.Seq
		for {
			lines := m.store.Next(p, after, scanBatch)
			if len(lines) == 0 {
				break
			}
			for _, l := range lines {
				if !m.mt.match(l.Text) {
					continue
				}
				s := l.Seq
				seqs = append(seqs, s)
				if first == 0 || s < first {
					first = s
				}
				last = max(last, s)
				if (s > from || (inclusive && s == from)) && (next == 0 || s < next) {
					next = s
				}
				if s < from && s > prev {
					prev = s
				}
			}
			after = lines[len(lines)-1].Seq
		}
	}
	if len(seqs) == 0 {
		m.match, m.matchPos, m.matchTotal = 0, 0, 0
		return false
	}
	dest := next
	if !forward {
		dest = prev
	}
	if dest == 0 {
		dest = first
		if !forward {
			dest = last
		}
	}
	pos := 0
	for _, s := range seqs {
		if s <= dest {
			pos++
		}
	}
	m.match, m.matchPos, m.matchTotal = dest, pos, len(seqs)
	m.follow = false
	m.top, m.topRow = dest, 0
	return true
}
