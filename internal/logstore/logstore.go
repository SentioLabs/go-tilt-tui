// Package logstore keeps Tilt log lines in one ring buffer per resource.
package logstore

import (
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// logLine is termsafe.LogLine. Tests swap it while termsafe is a stub.
var logLine = termsafe.LogLine

// Seq orders lines across all resources. It only ever increases, and it
// can have gaps where lines were hidden or dropped.
type Seq uint64

// End is a Seq after every stored line. Prev(src, End, n) returns the
// newest n lines, which is what follow mode draws.
const End Seq = math.MaxUint64

// MaxLineBytes caps one line's raw text. Text past it is dropped up to the
// line's newline and the line ends with "…[truncated]".
const MaxLineBytes = 64 << 10

type Level uint8

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// Line is one log line, open or complete.
type Line struct {
	Seq      Seq
	Version  uint32 // bumps when an append or a progressID update changes Text
	Resource string // "" for global logs
	Time     time.Time
	Level    Level
	Text     string // already passed through termsafe.LogLine; no "\n"
}

// Source selects which lines a view reads.
type Source struct {
	Resource string // ignored when All is true
	All      bool
}

type Options struct {
	// MaxBytesPerResource caps each resource's buffer. 0 means 2 MiB.
	MaxBytesPerResource int

	// Hide reports whether a complete line should be dropped. It gets the
	// line's text with SGR codes removed. Matching lines are never stored.
	// An open line that starts matching after an append is removed, and
	// the rest of it is dropped up to its newline. nil keeps every line.
	Hide func(plain string) bool
}

const defaultMaxBytes = 2 << 20

type entry struct {
	line       Line
	span       string
	progressID string
	raw        string // only while open
	open       bool
	cost       int
}

// buffer holds one resource's lines in ascending Seq order. Entries before
// head were evicted and are compacted away once they outnumber the live ones.
type buffer struct {
	entries []entry
	head    int
	bytes   int
}

// live returns the entries still readable.
func (b *buffer) live() []entry { return b.entries[b.head:] }

// search returns the index into live() of the first entry with Seq >= seq.
func (b *buffer) search(seq Seq) int {
	live := b.live()
	return sort.Search(len(live), func(i int) bool { return live[i].line.Seq >= seq })
}

// find returns the index into live() of the entry with Seq == seq.
func (b *buffer) find(seq Seq) (int, bool) {
	i := b.search(seq)
	live := b.live()
	return i, i < len(live) && live[i].line.Seq == seq
}

// remove deletes live()[i] and takes its cost off the byte total.
func (b *buffer) remove(i int) {
	j := b.head + i
	b.bytes -= b.entries[j].cost
	b.entries = slices.Delete(b.entries, j, j+1)
}

// compact moves the live entries to the front so evicted ones can be freed.
func (b *buffer) compact() {
	n := copy(b.entries, b.entries[b.head:])
	clear(b.entries[n:])
	b.entries = b.entries[:n]
	b.head = 0
}

// findProgress returns the index into live() of the line a segment with
// progressID replaces. It walks back from the newest entry past other
// spans' lines and stops at the first same-span line without a progressID.
func (b *buffer) findProgress(spanID, progressID string) (int, bool) {
	if progressID == "" {
		return 0, false
	}
	live := b.live()
	for i := len(live) - 1; i >= 0; i-- {
		e := &live[i]
		if e.span != spanID {
			continue
		}
		if e.progressID == "" {
			return 0, false
		}
		if e.progressID == progressID {
			return i, true
		}
	}
	return 0, false
}

// spanState tracks one span's open line and whether its input is being
// dropped up to the next "\n".
type spanState struct {
	resource string
	openSeq  Seq // 0 when no open line
	level    Level
	dropping bool
}

type Store struct {
	opts       Options
	maxBytes   int
	buffers    map[string]*buffer
	spans      map[string]string // span ID -> resource
	state      map[string]*spanState
	nextSeq    Seq
	checkpoint int
	gen        uint64
	resets     uint64
	dirty      bool // set by any change to a buffer during Append
}

func New(opts Options) *Store {
	s := &Store{opts: opts, maxBytes: opts.MaxBytesPerResource}
	if s.maxBytes <= 0 {
		s.maxBytes = defaultMaxBytes
	}
	s.reset()
	return s
}

// reset drops every buffer, open line and span, and restarts Seq at 1.
func (s *Store) reset() {
	s.buffers = map[string]*buffer{}
	s.spans = map[string]string{}
	s.state = map[string]*spanState{}
	s.nextSeq = 1
	s.checkpoint = 0
}

// Append applies the logList of one /ws/view message. A complete snapshot
// (v.IsComplete) resets the store first.
func (s *Store) Append(v tilt.View) {
	s.dirty = false
	if v.IsComplete {
		s.reset()
		s.resets++
		s.dirty = true
	}
	if v.LogList != nil {
		s.applyLogList(v.LogList)
	}
	if s.dirty {
		s.gen++
	}
}

func (s *Store) applyLogList(ll *tilt.LogList) {
	for id, sp := range ll.Spans {
		s.spans[id] = sp.ManifestName
	}
	// Segments before the held checkpoint were already applied.
	skip := 0
	if ll.FromCheckpoint < s.checkpoint {
		skip = min(s.checkpoint-ll.FromCheckpoint, len(ll.Segments))
	}
	for i := skip; i < len(ll.Segments); i++ {
		seg := &ll.Segments[i]
		s.applySegment(seg.SpanID, seg.Text, parseLevel(seg.Level), seg.Fields["progressID"], seg.Time)
	}
	if ll.ToCheckpoint > s.checkpoint {
		s.checkpoint = ll.ToCheckpoint
	}
}

func parseLevel(level string) Level {
	switch level {
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	}
	return LevelInfo
}

// applySegment splits one segment's text on "\n" and feeds each piece to
// the span's line state. A piece before a "\n" completes a line and a
// trailing piece without one leaves the line open.
func (s *Store) applySegment(spanID, text string, level Level, progressID string, at time.Time) {
	resource, ok := s.spans[spanID]
	if !ok {
		return
	}
	st := s.state[spanID]
	if st == nil {
		st = &spanState{resource: resource}
		s.state[spanID] = st
	}
	for len(text) > 0 {
		piece, complete := text, false
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			piece, complete = text[:i], true
			text = text[i+1:]
		} else {
			text = ""
		}
		s.feed(spanID, st, piece, complete, level, progressID, at)
	}
}

// feed applies one piece of a segment to the span's open line, or starts a
// new line for it. A span that is dropping discards input up to the next
// "\n".
func (s *Store) feed(spanID string, st *spanState, piece string, complete bool, level Level, progressID string, at time.Time) {
	if st.dropping {
		st.dropping = !complete
		return
	}
	if st.openSeq != 0 && st.level != level {
		s.closeOpen(st)
	}
	if st.openSeq != 0 {
		s.continueOpen(st, piece, complete)
		return
	}
	s.startLine(spanID, st, piece, complete, level, progressID, at)
}

// closeOpen completes the span's open line as it is.
func (s *Store) closeOpen(st *spanState) {
	b := s.buffers[st.resource]
	i, ok := b.find(st.openSeq)
	st.openSeq = 0
	if !ok {
		return
	}
	e := &b.live()[i]
	e.raw = ""
	e.open = false
	s.recost(b, e)
}

// continueOpen appends piece to the span's open line.
func (s *Store) continueOpen(st *spanState, piece string, complete bool) {
	b := s.buffers[st.resource]
	i, ok := b.find(st.openSeq)
	if !ok {
		st.openSeq = 0
		return
	}
	s.update(b, i, st, b.live()[i].raw+piece, complete)
}

// startLine stores a new line for the span, or replaces an earlier line
// that carries the same progressID.
func (s *Store) startLine(spanID string, st *spanState, piece string, complete bool, level Level, progressID string, at time.Time) {
	b := s.buffer(st.resource)
	if i, ok := b.findProgress(spanID, progressID); ok {
		e := &b.live()[i]
		e.line.Time = at
		e.line.Level = level
		s.update(b, i, st, piece, complete)
		return
	}
	cut, text, truncated, hidden := s.render(piece)
	if hidden {
		st.dropping = !complete
		return
	}
	e := entry{
		line:       Line{Seq: s.nextSeq, Resource: st.resource, Time: at, Level: level},
		span:       spanID,
		progressID: progressID,
	}
	s.nextSeq++
	finish(&e, st, cut, text, truncated, complete)
	e.cost = cost(&e)
	b.entries = append(b.entries, e)
	b.bytes += e.cost
	s.dirty = true
	s.evict(b)
}

// update gives the line at live()[i] the raw text raw. A line that Hide now
// matches is removed and the rest of its input is dropped.
func (s *Store) update(b *buffer, i int, st *spanState, raw string, complete bool) {
	cut, text, truncated, hidden := s.render(raw)
	if hidden {
		b.remove(i)
		st.openSeq = 0
		st.dropping = !complete
		s.dirty = true
		return
	}
	e := &b.live()[i]
	e.line.Version++
	finish(e, st, cut, text, truncated, complete)
	s.recost(b, e)
	s.evict(b)
}

// render computes a line's Text from its raw text. It cuts raw at
// MaxLineBytes, filters it and asks Hide about the plain result. The
// truncation marker is added after the Hide check.
func (s *Store) render(raw string) (cut, text string, truncated, hidden bool) {
	cut = raw
	if len(raw) > MaxLineBytes {
		cut, truncated = cutAtRune(raw, MaxLineBytes), true
	}
	text = logLine(cut)
	hidden = s.opts.Hide != nil && s.opts.Hide(ansi.Strip(text))
	if truncated {
		text += truncatedMarker
	}
	return cut, text, truncated, hidden
}

// cutAtRune returns s[:n] moved back to the nearest rune start.
func cutAtRune(s string, n int) string {
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// finish writes a rendered line into e and updates the span's open state.
// A truncated line is complete, and the span drops the rest of its input.
func finish(e *entry, st *spanState, cut, text string, truncated, complete bool) {
	e.line.Text = text
	e.raw = ""
	e.open = !complete && !truncated
	st.openSeq = 0
	if e.open {
		e.raw = cut
		st.openSeq = e.line.Seq
		st.level = e.line.Level
	}
	st.dropping = truncated && !complete
}

func (s *Store) buffer(resource string) *buffer {
	b := s.buffers[resource]
	if b == nil {
		b = &buffer{}
		s.buffers[resource] = b
	}
	return b
}

func cost(e *entry) int {
	c := entryOverhead + len(e.line.Text)
	if e.open {
		c += len(e.raw)
	}
	return c
}

// recost updates e's cost and the buffer's byte total after e changed.
func (s *Store) recost(b *buffer, e *entry) {
	b.bytes -= e.cost
	e.cost = cost(e)
	b.bytes += e.cost
	s.dirty = true
}

// evict drops the oldest entries until b fits the cap. Evicting a span's
// open line makes the span drop the rest of that line's input.
func (s *Store) evict(b *buffer) {
	for b.bytes > s.maxBytes && b.head < len(b.entries) {
		e := &b.entries[b.head]
		if e.open {
			if st := s.state[e.span]; st != nil && st.openSeq == e.line.Seq {
				st.openSeq = 0
				st.dropping = true
			}
		}
		b.bytes -= e.cost
		*e = entry{}
		b.head++
		s.dirty = true
	}
	if b.head > len(b.entries)/2 {
		b.compact()
	}
}

const (
	entryOverhead   = 64
	truncatedMarker = "…[truncated]"
)

// Next returns up to n lines from src with Seq > after, oldest first.
func (s *Store) Next(src Source, after Seq, n int) []Line {
	if n <= 0 || after == End {
		return nil
	}
	var out []Line
	for _, b := range s.sources(src) {
		live := b.live()
		i := b.search(after + 1)
		out = appendLines(out, live[i:min(i+n, len(live))])
	}
	return trimMerged(out, n, false)
}

// Prev returns up to n lines from src with Seq < before, oldest first.
func (s *Store) Prev(src Source, before Seq, n int) []Line {
	if n <= 0 {
		return nil
	}
	var out []Line
	for _, b := range s.sources(src) {
		live := b.live()
		i := b.search(before)
		out = appendLines(out, live[max(i-n, 0):i])
	}
	return trimMerged(out, n, true)
}

// Rank returns how many lines in src come before seq, and the total.
func (s *Store) Rank(src Source, seq Seq) (pos, total int) {
	for _, b := range s.sources(src) {
		pos += b.search(seq)
		total += len(b.live())
	}
	return pos, total
}

// sources returns the buffers src reads from.
func (s *Store) sources(src Source) []*buffer {
	if !src.All {
		b := s.buffers[src.Resource]
		if b == nil {
			return nil
		}
		return []*buffer{b}
	}
	bs := make([]*buffer, 0, len(s.buffers))
	for _, b := range s.buffers {
		bs = append(bs, b)
	}
	return bs
}

func appendLines(out []Line, entries []entry) []Line {
	for i := range entries {
		out = append(out, entries[i].line)
	}
	return out
}

// trimMerged sorts candidates from several buffers by Seq and keeps n of
// them: the newest when fromEnd is set, otherwise the oldest.
func trimMerged(lines []Line, n int, fromEnd bool) []Line {
	sort.Slice(lines, func(i, j int) bool { return lines[i].Seq < lines[j].Seq })
	if len(lines) > n {
		if fromEnd {
			lines = lines[len(lines)-n:]
		} else {
			lines = lines[:n]
		}
	}
	return lines
}

// Generation increases on every change, so views can skip redundant work.
func (s *Store) Generation() uint64 { return s.gen }

// Resets counts complete snapshots applied so far. A view that sees it
// change must drop every cache keyed by Seq, because a reset restarts Seq.
func (s *Store) Resets() uint64 { return s.resets }
