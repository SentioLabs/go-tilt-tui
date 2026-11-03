package logview

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// wrap splits one stored line into rows. The first row is w cells wide.
// Each later row starts with "↳ " and holds w-2 cells. text holds only SGR
// escapes (it is termsafe.LogLine output), and the SGR state active at a
// break is re-emitted after the "↳ " so colours continue on the next row.
func wrap(text string, w int) []string {
	w = max(w, 4)
	var (
		rows  []string
		cur   strings.Builder
		sgr   []string
		col   int
		limit = w
		state = byte(ansi.NormalState)
	)
	for len(text) > 0 {
		seq, width, n, next := ansi.DecodeSequence(text, state, nil)
		state = next
		text = text[n:]
		if strings.HasPrefix(seq, "\x1b") {
			cur.WriteString(seq)
			if seq == "\x1b[m" || seq == "\x1b[0m" {
				sgr = sgr[:0]
			} else {
				sgr = append(sgr, seq)
			}
			continue
		}
		if col > 0 && col+width > limit {
			rows = append(rows, cur.String())
			cur.Reset()
			cur.WriteString("↳ ")
			for _, s := range sgr {
				cur.WriteString(s)
			}
			col, limit = 0, w-2
		}
		cur.WriteString(seq)
		col += width
	}
	return append(rows, cur.String())
}
