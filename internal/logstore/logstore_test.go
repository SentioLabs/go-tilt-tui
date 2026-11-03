package logstore

import "github.com/sentiolabs/go-tilt-tui/internal/tilt"

// --- Contract assertions ---

var (
	_ func(Options) *Store                  = New
	_ func(*Store, tilt.View)               = (*Store).Append
	_ func(*Store, Source, Seq, int) []Line = (*Store).Next
	_ func(*Store, Source, Seq, int) []Line = (*Store).Prev
	_ func(*Store, Source, Seq) (int, int)  = (*Store).Rank
	_ func(*Store) uint64                   = (*Store).Generation
	_ func(*Store) uint64                   = (*Store).Resets
	_ string                                = Line{}.Text
	_ uint32                                = Line{}.Version
	_ int                                   = MaxLineBytes
)
