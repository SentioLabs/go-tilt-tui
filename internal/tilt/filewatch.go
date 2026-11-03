package tilt

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// FileWatch is one Tilt FileWatch object, reduced to what the file-watch
// panel shows. Every string is raw Tilt data: pass it through
// termsafe.Text before drawing.
type FileWatch struct {
	Name         string      // metadata.name, e.g. "local:api"
	Resource     string      // metadata.annotations["tilt.dev/resource"]; "" when absent
	WatchedPaths []string    // spec.watchedPaths
	Ignores      []IgnoreDef // spec.ignores
	LastEvent    time.Time   // status.lastEventTime; zero when Tilt sent null
	Events       []FileEvent // status.fileEvents, in Tilt's order
	Error        string      // status.error
}

// IgnoreDef is one spec.ignores entry: patterns relative to BasePath.
type IgnoreDef struct {
	BasePath string   `json:"basePath"`
	Patterns []string `json:"patterns,omitempty"`
}

// FileEvent is one status.fileEvents entry.
type FileEvent struct {
	Time      time.Time `json:"time"`
	SeenFiles []string  `json:"seenFiles,omitempty"`
}

// fileWatchList is the part of `tilt get filewatches -o json` this
// package reads. Items is a pointer so a missing key is detectable.
type fileWatchList struct {
	Items *[]struct {
		Metadata ObjectMeta `json:"metadata"`
		Spec     struct {
			WatchedPaths []string    `json:"watchedPaths"`
			Ignores      []IgnoreDef `json:"ignores"`
		} `json:"spec"`
		Status struct {
			LastEventTime *time.Time  `json:"lastEventTime"`
			FileEvents    []FileEvent `json:"fileEvents"`
			Error         string      `json:"error"`
		} `json:"status"`
	} `json:"items"`
}

// ParseFileWatches decodes the output of `tilt get filewatches -o json`.
// It returns ErrUnknownFormat when raw isn't JSON or has no "items" key.
// The result is sorted by Resource, then Name.
func ParseFileWatches(raw []byte) ([]FileWatch, error) {
	var l fileWatchList
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("file watches: %w", ErrUnknownFormat)
	}
	if l.Items == nil {
		return nil, fmt.Errorf("file watches: %w: no items", ErrUnknownFormat)
	}
	out := make([]FileWatch, 0, len(*l.Items))
	for _, it := range *l.Items {
		w := FileWatch{
			Name:         it.Metadata.Name,
			Resource:     it.Metadata.Annotations["tilt.dev/resource"],
			WatchedPaths: it.Spec.WatchedPaths,
			Ignores:      it.Spec.Ignores,
			Events:       it.Status.FileEvents,
			Error:        it.Status.Error,
		}
		if it.Status.LastEventTime != nil {
			w.LastEvent = *it.Status.LastEventTime
		}
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b FileWatch) int {
		return cmp.Or(strings.Compare(a.Resource, b.Resource), strings.Compare(a.Name, b.Name))
	})
	return out, nil
}
