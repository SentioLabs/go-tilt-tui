// Package config loads ~/.config/tilt-tui/config.toml (or config.json).
package config

import "regexp"

// Config uses tilt-tui's key names so existing settings carry over.
type Config struct {
	Theme                  string              `toml:"theme" json:"theme"`
	TiltBinaryPath         string              `toml:"tiltBinaryPath" json:"tiltBinaryPath"`
	LogFilters             map[string][]string `toml:"logFilters" json:"logFilters"`
	LogMaxBytesPerResource int                 `toml:"logMaxBytesPerResource" json:"logMaxBytesPerResource"`
}

// Filter is one named logFilters entry, compiled with RE2.
type Filter struct {
	Name     string
	Patterns []*regexp.Regexp
}

// Loaded is the result of Load.
type Loaded struct {
	Config   Config
	Filters  []Filter
	Path     string   // file read, "" when none exists
	Warnings []string // e.g. "convert config.json to config.toml"
}

// Dir returns $XDG_CONFIG_HOME/tilt-tui when XDG_CONFIG_HOME is set, else
// ~/.config/tilt-tui, on every OS.
func Dir() (string, error) { panic("T0 stub") }

// Load reads dir/config.toml, falling back to dir/config.json. A missing
// file is not an error. An invalid regex is, and names the filter.
func Load(dir string) (Loaded, error) { panic("T0 stub") }
