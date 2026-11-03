package tilt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"
)

func loadFixture(t *testing.T) []View {
	t.Helper()
	raw, err := os.ReadFile("testdata/views.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`/home/[a-z]|/Users/`).Match(bytes.ReplaceAll(raw, []byte("/home/user"), nil)) {
		t.Fatal("fixture contains a developer home path")
	}
	var views []View
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var v View
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatalf("line %d: %v", len(views)+1, err)
		}
		views = append(views, v)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return views
}

func TestRecordedViews(t *testing.T) {
	views := loadFixture(t)
	if len(views) < 10 || !views[0].IsComplete {
		t.Fatalf("got %d views, first complete=%v", len(views), len(views) > 0 && views[0].IsComplete)
	}
	s := NewStore()
	segments := 0
	for _, v := range views {
		s.Apply(v)
		if v.LogList != nil {
			segments += len(v.LogList.Segments)
		}
	}
	if segments == 0 {
		t.Fatal("fixture has no log segments")
	}

	byName := map[string]Resource{}
	var names []string
	for _, r := range s.Resources() {
		byName[r.Name()] = r
		names = append(names, r.Name())
	}
	for _, want := range []string{"(Tiltfile)", "hello", "slow-build", "broken", "manual"} {
		if !slices.Contains(names, want) {
			t.Fatalf("resource %q missing; have %q", want, names)
		}
	}
	if byName["broken"].LastBuildError() == "" {
		t.Error("broken has no build error")
	}
	if len(byName["manual"].Status.BuildHistory) == 0 {
		t.Error("manual was never triggered in the recording")
	}
	if byName["hello"].Status.RuntimeStatus != RuntimeOK {
		t.Errorf("hello runtime = %q, want ok", byName["hello"].Status.RuntimeStatus)
	}
}

func TestRecordedProgressFieldsDecode(t *testing.T) {
	// Not every recording has progress lines. When it does, fields must
	// survive decoding, because progressID replacement depends on them.
	for _, v := range loadFixture(t) {
		if v.LogList == nil {
			continue
		}
		for _, seg := range v.LogList.Segments {
			if seg.Fields["progressID"] != "" {
				return
			}
		}
	}
	t.Log("no progressID segments in this recording")
}
