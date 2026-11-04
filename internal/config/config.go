// Package config loads ~/.config/tilt-tui/config.toml (or config.json).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"
)

// minBytesPerResource is the smallest per-resource log buffer Load accepts.
const minBytesPerResource = 256 << 10

// WheelModes are the mouseWheelList values, the default first.
var WheelModes = []string{"cursor", "scroll"}

// Config uses tilt-tui's key names so existing settings carry over.
type Config struct {
	Theme                  string              `toml:"theme" json:"theme"`
	TiltBinaryPath         string              `toml:"tiltBinaryPath" json:"tiltBinaryPath"`
	LogFilters             map[string][]string `toml:"logFilters" json:"logFilters"`
	LogMaxBytesPerResource int                 `toml:"logMaxBytesPerResource" json:"logMaxBytesPerResource"`
	MouseWheelList         string              `toml:"mouseWheelList" json:"mouseWheelList"`
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
func Dir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "tilt-tui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find config dir: %w", err)
	}
	return filepath.Join(home, ".config", "tilt-tui"), nil
}

// Load reads dir/config.toml, falling back to dir/config.json. A missing
// file is not an error. An invalid regex is, and names the filter.
//
// Patterns compile with RE2, which has no lookaround or backreferences, so
// some tilt-tui JavaScript patterns need rewriting.
func Load(dir string) (Loaded, error) {
	var l Loaded
	path := filepath.Join(dir, "config.toml")
	md, err := toml.DecodeFile(path, &l.Config)
	switch {
	case err == nil:
		l.Path = path
		for _, k := range md.Undecoded() {
			l.Warnings = append(l.Warnings, fmt.Sprintf("unknown config key %q in %s", k.String(), path))
		}
	case errors.Is(err, fs.ErrNotExist):
		found, err := loadJSON(filepath.Join(dir, "config.json"), &l)
		if err != nil || !found {
			return Loaded{}, err
		}
	default:
		return Loaded{}, fmt.Errorf("read %s: %w", path, err)
	}

	filters, err := compileFilters(l.Config.LogFilters)
	if err != nil {
		return Loaded{}, err
	}
	l.Filters = filters

	switch n := l.Config.LogMaxBytesPerResource; {
	case n < 0:
		return Loaded{}, fmt.Errorf("logMaxBytesPerResource in %s is %d, want 0 or more", l.Path, n)
	case n > 0 && n < minBytesPerResource:
		l.Config.LogMaxBytesPerResource = minBytesPerResource
		l.Warnings = append(l.Warnings, fmt.Sprintf(
			"logMaxBytesPerResource raised from %d to %d (the minimum)", n, minBytesPerResource))
	}

	switch v := l.Config.MouseWheelList; {
	case v == "":
		l.Config.MouseWheelList = WheelModes[0]
	case !slices.Contains(WheelModes, v):
		l.Config.MouseWheelList = WheelModes[0]
		l.Warnings = append(l.Warnings, fmt.Sprintf("mouseWheelList %q is not cursor or scroll, using cursor", v))
	}
	return l, nil
}

// loadJSON decodes path into l.Config. It reports false with no error when
// path doesn't exist. Unknown JSON keys are ignored.
func loadJSON(path string, l *Loaded) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &l.Config); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	l.Path = path
	l.Warnings = append(l.Warnings, "read config.json; tilt-tui-go prefers config.toml in the same directory")
	return true, nil
}

// compileFilters compiles each logFilters entry with RE2. Filters come back
// sorted by name, and each keeps its patterns in file order.
func compileFilters(raw map[string][]string) ([]Filter, error) {
	var filters []Filter
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		f := Filter{Name: name}
		for _, p := range raw[name] {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("logFilters %q: pattern %q: %w (RE2 has no lookaround or backreferences)", name, p, err)
			}
			f.Patterns = append(f.Patterns, re)
		}
		filters = append(filters, f)
	}
	return filters, nil
}
