package logview

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// benchStore holds 44 resources of 20 000 lines each. Every tenth line
// is coloured, every twentieth is wide text and every fiftieth wraps.
func benchStore(b *testing.B) *logstore.Store {
	b.Helper()
	s := logstore.New(logstore.Options{MaxBytesPerResource: 8 << 20})
	const resources, perResource = 44, 20000
	spans := map[string]any{}
	for r := range resources {
		spans[fmt.Sprintf("s%d", r)] = map[string]string{"manifestName": fmt.Sprintf("res-%02d", r)}
	}
	type seg struct {
		SpanID string    `json:"spanId"`
		Time   time.Time `json:"time"`
		Text   string    `json:"text"`
	}
	base := strings.Repeat("abcdefghij", 8)[:79]
	for chunk := 0; chunk < perResource; chunk += 1000 {
		segs := make([]seg, 0, resources*1000)
		for i := chunk; i < chunk+1000; i++ {
			for r := range resources {
				text := base
				switch {
				case i%50 == 0:
					text = strings.Repeat(base+" ", 5)[:399]
				case i%20 == 0:
					text = strings.Repeat("日本語", 13)[:78]
				case i%10 == 0:
					text = "\x1b[32m" + base[:70] + "\x1b[0m"
				}
				segs = append(segs, seg{SpanID: fmt.Sprintf("s%d", r), Time: time.Unix(int64(i), 0), Text: text + "\n"})
			}
		}
		raw, _ := json.Marshal(map[string]any{"isComplete": chunk == 0, "logList": map[string]any{"segments": segs, "spans": spans}})
		var v tilt.View
		if err := json.Unmarshal(raw, &v); err != nil {
			b.Fatal(err)
		}
		s.Append(v)
	}
	return s
}

func BenchmarkRedraw(b *testing.B) {
	m := New(benchStore(b), mono(b))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.View(140, 50, true)
	b.ResetTimer()
	for b.Loop() {
		m.View(140, 50, true)
	}
}

// BenchmarkPageDown pages down once per iteration on the All source.
// The target is under 2 ms per keypress.
func BenchmarkPageDown(b *testing.B) {
	m := New(benchStore(b), mono(b))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.View(140, 50, true)
	m.follow = false
	m.top, m.topRow = 0, 0
	m.normalize()
	start := m.top
	b.ResetTimer()
	for b.Loop() {
		m.down(50)
		if m.top == start {
			b.Fatal("down(50) did not move")
		}
		b.StopTimer()
		m.top, m.topRow = start, 0
		b.StartTimer()
	}
}

// BenchmarkRedrawSearch redraws with an active search whose matches are
// on screen. Each round sets a new query, so every visible line is
// matched and highlighted again.
func BenchmarkRedrawSearch(b *testing.B) {
	m := New(benchStore(b), mono(b))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.View(140, 50, true)
	queries := []string{"cde", "CDE", "日本"}
	b.ResetTimer()
	i := 0
	for b.Loop() {
		m.SetQuery(queries[i%len(queries)])
		i++
		m.View(140, 50, true)
	}
}

// BenchmarkNextMatch times one n press: a walk over every line of every
// resource.
func BenchmarkNextMatch(b *testing.B) {
	m := New(benchStore(b), mono(b))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.View(140, 50, true)
	var names []string
	for r := range 44 {
		names = append(names, fmt.Sprintf("res-%02d", r))
	}
	parts := Parts(m.Source(), names)
	m.SetQuery("日本")
	b.ResetTimer()
	for b.Loop() {
		if !m.NextMatch(parts) {
			b.Fatal("no match")
		}
	}
}
