package config

// --- Contract assertions ---

var (
	_ func() (string, error)       = Dir
	_ func(string) (Loaded, error) = Load
)
