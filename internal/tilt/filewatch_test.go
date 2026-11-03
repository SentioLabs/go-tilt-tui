package tilt

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// --- Contract assertions ---

var _ func([]byte) ([]FileWatch, error) = ParseFileWatches

// --- Behavior tests (added by the file-watch task) ---

const fileWatchJSON = `{"apiVersion":"v1","kind":"List","metadata":{"resourceVersion":""},"items":[
 {"metadata":{"name":"local:api","annotations":{"tilt.dev/resource":"api"}},
  "spec":{"ignores":[{"basePath":"/work","patterns":["src/*.tmp"]}],"watchedPaths":["/work/src"]},
  "status":{"fileEvents":[{"seenFiles":["/work/src/b.txt"],"time":"2026-11-03T23:05:44.986443Z"}],
            "lastEventTime":"2026-11-03T23:05:44.986443Z","error":"too many files"}},
 {"metadata":{"name":"configs:(Tiltfile)","annotations":{"tilt.dev/resource":"(Tiltfile)"}},
  "spec":{"watchedPaths":["/work/.tiltignore","/work/Tiltfile"]},
  "status":{"lastEventTime":null}},
 {"metadata":{"name":"orphan"},"spec":{},"status":{}}]}`

func TestParseFileWatches(t *testing.T) {
	got, err := ParseFileWatches([]byte(fileWatchJSON))
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by Resource, then Name: "" < "(Tiltfile)" < "api".
	names := []string{}
	for _, w := range got {
		names = append(names, w.Name)
	}
	if want := []string{"orphan", "configs:(Tiltfile)", "local:api"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("order %v, want %v", names, want)
	}
	api := got[2]
	at := time.Date(2026, 11, 3, 23, 5, 44, 986443000, time.UTC)
	want := FileWatch{
		Name: "local:api", Resource: "api",
		WatchedPaths: []string{"/work/src"},
		Ignores:      []IgnoreDef{{BasePath: "/work", Patterns: []string{"src/*.tmp"}}},
		LastEvent:    at,
		Events:       []FileEvent{{Time: at, SeenFiles: []string{"/work/src/b.txt"}}},
		Error:        "too many files",
	}
	if !reflect.DeepEqual(api, want) {
		t.Fatalf("api = %+v\nwant  %+v", api, want)
	}
	if !got[1].LastEvent.IsZero() || got[1].Resource != "(Tiltfile)" {
		t.Fatalf("tiltfile watch = %+v", got[1])
	}
}

func TestParseFileWatchesEmptyList(t *testing.T) {
	got, err := ParseFileWatches([]byte(`{"kind":"List","items":[]}`))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestParseFileWatchesUnknownFormat(t *testing.T) {
	for _, raw := range []string{``, `nope`, `{}`, `{"items":{}}`, `[]`} {
		if _, err := ParseFileWatches([]byte(raw)); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%q: err = %v, want ErrUnknownFormat", raw, err)
		}
	}
}
