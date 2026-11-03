package tilt

// --- Contract assertions ---

var _ func([]byte) ([]FileWatch, error) = ParseFileWatches

// --- Behavior tests (added by the file-watch task) ---
