package tilt

import "time"

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

// ParseFileWatches decodes the output of `tilt get filewatches -o json`.
// It returns ErrUnknownFormat when raw isn't JSON or has no "items" key.
// The result is sorted by Resource, then Name.
func ParseFileWatches(raw []byte) ([]FileWatch, error) { panic("T0 stub") }
