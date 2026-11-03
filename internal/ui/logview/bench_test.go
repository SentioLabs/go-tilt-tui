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

func BenchmarkRedraw(b *testing.B) {
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
	m := New(s, mono(b))
	m.SetSource(logstore.Source{All: true}, "All Resources")
	m.View(140, 50, true)
	b.ResetTimer()
	for b.Loop() {
		m.View(140, 50, true)
	}
}
