package logstore

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

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

func TestMain(m *testing.M) {
	logLine = func(s string) string { return s } // remove once termsafe.LogLine lands
	os.Exit(m.Run())
}

// --- Behaviour tests ---

type seg struct {
	span, text, level, progress string
}

// view builds a tilt.View through JSON, because tilt.LogList uses anonymous
// structs that are awkward to write as literals.
func view(t testing.TB, complete bool, from, to int, spans map[string]string, segs ...seg) tilt.View {
	t.Helper()
	type jseg struct {
		SpanID string            `json:"spanId"`
		Time   time.Time         `json:"time"`
		Text   string            `json:"text"`
		Level  string            `json:"level,omitempty"`
		Fields map[string]string `json:"fields,omitempty"`
	}
	js := make([]jseg, len(segs))
	for i, s := range segs {
		js[i] = jseg{SpanID: s.span, Time: time.Unix(int64(1000+i), 0), Text: s.text, Level: s.level}
		if s.progress != "" {
			js[i].Fields = map[string]string{"progressID": s.progress}
		}
	}
	sp := map[string]map[string]string{}
	for id, name := range spans {
		sp[id] = map[string]string{"manifestName": name}
	}
	raw, err := json.Marshal(map[string]any{
		"isComplete": complete,
		"logList": map[string]any{
			"segments": js, "spans": sp, "fromCheckpoint": from, "toCheckpoint": to,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var v tilt.View
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

var spans = map[string]string{"a": "api", "a2": "api", "w": "web", "g": ""}

func texts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

func all(s *Store) []Line           { return s.Next(Source{All: true}, 0, 1<<20) }
func res(s *Store, r string) []Line { return s.Next(Source{Resource: r}, 0, 1<<20) }

func equal(t *testing.T, got []Line, want ...string) {
	t.Helper()
	if g := texts(got); !slices.Equal(g, want) {
		t.Fatalf("lines = %q, want %q", g, want)
	}
}

func TestSplitAndOpenLine(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: "one\ntw"}))
	got := res(s, "api")
	equal(t, got, "one", "tw")
	open := got[1]
	s.Append(view(t, false, 1, 2, spans, seg{span: "a", text: "o\n"}))
	got = res(s, "api")
	equal(t, got, "one", "two")
	if got[1].Seq != open.Seq || got[1].Version <= open.Version {
		t.Fatalf("continued line Seq/Version = %d/%d, was %d/%d", got[1].Seq, got[1].Version, open.Seq, open.Version)
	}
}

func TestOpenLineNeedsSameLevel(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 2, spans, seg{span: "a", text: "x"}, seg{span: "a", text: "y\n", level: "WARN"}))
	got := res(s, "api")
	equal(t, got, "x", "y")
	if got[1].Level != LevelWarn {
		t.Fatalf("level = %v, want LevelWarn", got[1].Level)
	}
}

func TestOpenLinePerSpan(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 3, spans,
		seg{span: "a", text: "x"}, seg{span: "w", text: "y\n"}, seg{span: "a", text: "z\n"}))
	equal(t, all(s), "xz", "y")
}

func TestUnknownSpanDroppedAndGlobalBuffer(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 2, spans, seg{span: "nope", text: "lost\n"}, seg{span: "g", text: "global\n"}))
	equal(t, all(s), "global")
	equal(t, res(s, ""), "global")
	equal(t, res(s, "api"))
}

func TestSpanTablePersistsAcrossMessages(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 0, spans))
	s.Append(view(t, false, 0, 1, nil, seg{span: "a", text: "later\n"}))
	equal(t, res(s, "api"), "later")
}

func TestCheckpointSkipsDuplicates(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 2, spans, seg{span: "a", text: "1\n"}, seg{span: "a", text: "2\n"}))
	s.Append(view(t, false, 1, 3, spans, seg{span: "a", text: "2\n"}, seg{span: "a", text: "3\n"}))
	equal(t, res(s, "api"), "1", "2", "3")
}

func TestCompleteSnapshotResets(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 2, spans, seg{span: "a", text: "old\n"}, seg{span: "a", text: "open"}))
	r0 := s.Resets()
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: "new\n"}))
	got := all(s)
	equal(t, got, "new")
	if got[0].Seq != 1 {
		t.Fatalf("Seq after reset = %d, want 1", got[0].Seq)
	}
	if s.Resets() != r0+1 {
		t.Fatalf("Resets = %d, want %d", s.Resets(), r0+1)
	}
	s.Append(tilt.View{IsComplete: true})
	equal(t, all(s))
}

func TestProgressIDReplacesInZone(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 4, spans,
		seg{span: "a", text: "start\n"},
		seg{span: "a", text: "p1 10%\n", progress: "p1"},
		seg{span: "a", text: "p2 5%\n", progress: "p2"},
		seg{span: "a", text: "p1 20%\n", progress: "p1"}))
	got := res(s, "api")
	equal(t, got, "start", "p1 20%", "p2 5%")
	if got[1].Version == 0 {
		t.Fatal("replaced line kept Version 0")
	}
}

func TestProgressIDStopsAtPlainLine(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 3, spans,
		seg{span: "a", text: "10%\n", progress: "p1"},
		seg{span: "a", text: "plain\n"},
		seg{span: "a", text: "20%\n", progress: "p1"}))
	equal(t, res(s, "api"), "10%", "plain", "20%")
}

func TestProgressIDIgnoresOtherSpans(t *testing.T) {
	s := New(Options{})
	// Same resource, different span: no match, and the other span's
	// lines don't end the zone either.
	s.Append(view(t, true, 0, 4, spans,
		seg{span: "a", text: "a 10%\n", progress: "p1"},
		seg{span: "a2", text: "b 50%\n", progress: "p1"},
		seg{span: "a2", text: "noise\n"},
		seg{span: "a", text: "a 20%\n", progress: "p1"}))
	equal(t, res(s, "api"), "a 20%", "b 50%", "noise")
}

func TestLineCap(t *testing.T) {
	s := New(Options{})
	long := strings.Repeat("x", MaxLineBytes+100)
	s.Append(view(t, true, 0, 3, spans,
		seg{span: "a", text: long}, seg{span: "a", text: "more\n"}, seg{span: "a", text: "next\n"}))
	got := res(s, "api")
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2", len(got))
	}
	want := strings.Repeat("x", MaxLineBytes) + "…[truncated]"
	if got[0].Text != want {
		t.Fatalf("capped line len %d, want %d with marker", len(got[0].Text), len(want))
	}
	if got[1].Text != "next" {
		t.Fatalf("line after cap = %q", got[1].Text)
	}
}

func TestLineCapRuneBoundary(t *testing.T) {
	s := New(Options{})
	long := strings.Repeat("x", MaxLineBytes-1) + "日本\n"
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: long}))
	got := res(s, "api")[0].Text
	if !utf8.ValidString(got) {
		t.Fatal("cut split a rune")
	}
}

func TestNoisyResourceDoesNotEvictOthers(t *testing.T) {
	s := New(Options{MaxBytesPerResource: 8 << 10})
	s.Append(view(t, true, 0, 1, spans, seg{span: "w", text: "crash log\n"}))
	segs := make([]seg, 10000)
	for i := range segs {
		segs[i] = seg{span: "a", text: fmt.Sprintf("noise %d\n", i)}
	}
	s.Append(view(t, false, 1, 10001, spans, segs...))
	equal(t, res(s, "web"), "crash log")
	got := res(s, "api")
	if len(got) == 0 || got[len(got)-1].Text != "noise 9999" {
		t.Fatal("newest noisy line missing")
	}
	var bytes int
	for _, l := range got {
		bytes += 64 + len(l.Text)
	}
	if bytes > 8<<10 {
		t.Fatalf("api buffer holds %d bytes, cap 8192", bytes)
	}
}

func TestEmptyLinesAreBounded(t *testing.T) {
	s := New(Options{MaxBytesPerResource: 8 << 10})
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: strings.Repeat("\n", 100000)}))
	if _, total := s.Rank(Source{Resource: "api"}, End); total > (8<<10)/64 {
		t.Fatalf("%d empty lines stored, want at most %d", total, (8<<10)/64)
	}
}

func hideSecret(plain string) bool { return strings.Contains(plain, "secret") }

func TestHideCompleteAndSplitLines(t *testing.T) {
	s := New(Options{Hide: hideSecret})
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: "keep\nhas secret\n"}))
	equal(t, res(s, "api"), "keep")

	// Split across segments with SGR between the halves of the word.
	s.Append(view(t, false, 1, 2, spans, seg{span: "a", text: "\x1b[31msec"}))
	equal(t, res(s, "api"), "keep", "\x1b[31msec")
	s.Append(view(t, false, 2, 4, spans, seg{span: "a", text: "\x1b[0mret and"}, seg{span: "a", text: " more\nafter\n"}))
	got := res(s, "api")
	equal(t, got, "keep", "after")
	if pos, total := s.Rank(Source{Resource: "api"}, End); pos != 2 || total != 2 {
		t.Fatalf("Rank = %d/%d, want 2/2", pos, total)
	}
	if prev := s.Prev(Source{All: true}, End, 10); len(prev) != 2 {
		t.Fatalf("Prev returned hidden lines: %q", texts(prev))
	}
}

func TestHiddenSplitLinesStoreNothing(t *testing.T) {
	s := New(Options{MaxBytesPerResource: 8 << 10, Hide: hideSecret})
	s.Append(view(t, true, 0, 1, spans, seg{span: "w", text: "visible\n"}))
	segs := make([]seg, 0, 20000)
	for range 10000 {
		segs = append(segs, seg{span: "a", text: "secret"}, seg{span: "a", text: "\n"})
	}
	s.Append(view(t, false, 1, 20001, spans, segs...))
	if _, total := s.Rank(Source{Resource: "api"}, End); total != 0 {
		t.Fatalf("%d hidden lines stored", total)
	}
	equal(t, res(s, "web"), "visible")
}

func TestEvictedOpenLineDropsRest(t *testing.T) {
	s := New(Options{MaxBytesPerResource: 4 << 10})
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: "open"}))
	filler := make([]seg, 200)
	for i := range filler {
		filler[i] = seg{span: "a2", text: fmt.Sprintf("fill %d\n", i)}
	}
	s.Append(view(t, false, 1, 201, spans, filler...))
	s.Append(view(t, false, 201, 203, spans, seg{span: "a", text: " tail\n"}, seg{span: "a", text: "fresh\n"}))
	for _, l := range res(s, "api") {
		if strings.Contains(l.Text, "tail") {
			t.Fatalf("continuation of an evicted open line was stored: %q", l.Text)
		}
	}
	if got := res(s, "api"); got[len(got)-1].Text != "fresh" {
		t.Fatal("line after the dropped tail missing")
	}
}

func TestSGRSplitAcrossSegments(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 2, spans, seg{span: "a", text: "\x1b[3"}, seg{span: "a", text: "1mred\n"}))
	equal(t, res(s, "api"), "\x1b[31mred")
}

func TestMergedReads(t *testing.T) {
	s := New(Options{})
	s.Append(view(t, true, 0, 5, spans,
		seg{span: "a", text: "a1\n"}, seg{span: "w", text: "w1\n"}, seg{span: "a", text: "a2\n"},
		seg{span: "g", text: "g1\n"}, seg{span: "w", text: "w2\n"}))
	src := Source{All: true}
	lines := all(s)
	equal(t, lines, "a1", "w1", "a2", "g1", "w2")
	equal(t, s.Next(src, lines[1].Seq, 2), "a2", "g1")
	equal(t, s.Prev(src, lines[3].Seq, 2), "w1", "a2")
	equal(t, s.Prev(src, End, 2), "g1", "w2")
	equal(t, s.Prev(Source{Resource: "api"}, End, 5), "a1", "a2")
	if pos, total := s.Rank(src, lines[2].Seq); pos != 2 || total != 5 {
		t.Fatalf("Rank = %d/%d, want 2/5", pos, total)
	}
}

func TestGeneration(t *testing.T) {
	s := New(Options{})
	g0 := s.Generation()
	s.Append(view(t, true, 0, 1, spans, seg{span: "a", text: "x\n"}))
	g1 := s.Generation()
	if g1 <= g0 {
		t.Fatal("Generation did not increase on append")
	}
	s.Append(tilt.View{})
	if s.Generation() != g1 {
		t.Fatal("Generation changed on an empty message")
	}
}

func FuzzAppend(f *testing.F) {
	f.Add("a", "x\ny", "", "", "w", "z\n", "WARN", "p1")
	f.Fuzz(func(t *testing.T, s1, t1, l1, p1, s2, t2, l2, p2 string) {
		s := New(Options{MaxBytesPerResource: 4 << 10, Hide: hideSecret})
		for i := range 3 {
			s.Append(view(t, i == 0, i, i+2, spans,
				seg{span: s1, text: t1, level: l1, progress: p1},
				seg{span: s2, text: t2, level: l2, progress: p2}))
		}
		for _, src := range []Source{{All: true}, {Resource: "api"}, {Resource: "web"}, {Resource: ""}} {
			lines := s.Next(src, 0, 1<<20)
			for i := 1; i < len(lines); i++ {
				if lines[i].Seq <= lines[i-1].Seq {
					t.Fatalf("Seq not ascending in %+v", src)
				}
			}
			for _, l := range lines {
				if strings.Contains(l.Text, "\n") || hideSecret(ansi.Strip(l.Text)) {
					t.Fatalf("bad stored line %q", l.Text)
				}
			}
		}
	})
}
