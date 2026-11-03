// Package logstore keeps Tilt log lines in one ring buffer per resource.
package logstore

import (
	"math"
	"time"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

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

type Store struct{}

func New(opts Options) *Store { panic("T0 stub") }

// Append applies the logList of one /ws/view message. A complete snapshot
// (v.IsComplete) resets the store first.
func (s *Store) Append(v tilt.View) { panic("T0 stub") }

// Next returns up to n lines from src with Seq > after, oldest first.
func (s *Store) Next(src Source, after Seq, n int) []Line { panic("T0 stub") }

// Prev returns up to n lines from src with Seq < before, oldest first.
func (s *Store) Prev(src Source, before Seq, n int) []Line { panic("T0 stub") }

// Rank returns how many lines in src come before seq, and the total.
func (s *Store) Rank(src Source, seq Seq) (pos, total int) { panic("T0 stub") }

// Generation increases on every change, so views can skip redundant work.
func (s *Store) Generation() uint64 { panic("T0 stub") }

// Resets counts complete snapshots applied so far. A view that sees it
// change must drop every cache keyed by Seq, because a reset restarts Seq.
func (s *Store) Resets() uint64 { panic("T0 stub") }
