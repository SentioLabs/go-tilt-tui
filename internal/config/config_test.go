package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Contract assertions ---

var (
	_ func() (string, error)       = Dir
	_ func(string) (Loaded, error) = Load
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if d, err := Dir(); err != nil || d != filepath.Join("/xdg", "tilt-tui") {
		t.Fatalf("Dir() = %q, %v", d, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if d, err := Dir(); err != nil || d != filepath.Join("/home/u", ".config", "tilt-tui") {
		t.Fatalf("Dir() = %q, %v", d, err)
	}
}

func TestLoadMissing(t *testing.T) {
	l, err := Load(t.TempDir())
	if err != nil || l.Path != "" || len(l.Filters) != 0 {
		t.Fatalf("Load(empty) = %+v, %v", l, err)
	}
}

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.toml", `
theme = "mono"
tiltBinaryPath = "/opt/tilt"
logMaxBytesPerResource = 4194304
mystery = 1

[logFilters]
probes = ["GET /healthz", "GET /readyz"]
alpha = ["^DEBUG"]
`)
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != filepath.Join(dir, "config.toml") || l.Config.Theme != "mono" ||
		l.Config.TiltBinaryPath != "/opt/tilt" || l.Config.LogMaxBytesPerResource != 4<<20 {
		t.Fatalf("Load = %+v", l)
	}
	if len(l.Filters) != 2 || l.Filters[0].Name != "alpha" || l.Filters[1].Name != "probes" {
		t.Fatalf("filters not sorted by name: %+v", l.Filters)
	}
	if !l.Filters[1].Patterns[1].MatchString("GET /readyz 200") {
		t.Fatal("pattern order or compile wrong")
	}
	if len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], `"mystery"`) {
		t.Fatalf("warnings = %q, want one for mystery", l.Warnings)
	}
}

func TestLoadJSONFallback(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", `{"theme":"tokyo-night","logFilters":{"x":["y"]},"extra":true}`)
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != filepath.Join(dir, "config.json") || l.Config.Theme != "tokyo-night" || len(l.Filters) != 1 {
		t.Fatalf("Load = %+v", l)
	}
	if len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], "config.toml") {
		t.Fatalf("warnings = %q", l.Warnings)
	}
}

func TestTOMLWinsOverJSON(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.toml", `theme = "mono"`)
	write(t, dir, "config.json", `{"theme":"default"}`)
	if l, err := Load(dir); err != nil || l.Config.Theme != "mono" || len(l.Warnings) != 0 {
		t.Fatalf("Load = %+v, %v", l, err)
	}
}

func TestBadRegexNamesFilter(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.toml", "[logFilters]\nnoisy = [\"(?<=x)y\"]\n")
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), `"noisy"`) || !strings.Contains(err.Error(), "(?<=x)y") {
		t.Fatalf("err = %v, want filter name and pattern", err)
	}
}

func TestParseErrorNamesFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.toml", "theme = ")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("err = %v", err)
	}
}

func TestMinimumBufferSize(t *testing.T) {
	tests := []struct {
		in, want int
		warn     bool
	}{
		{0, 0, false},
		{1000, 256 << 10, true},
		{256 << 10, 256 << 10, false},
		{8 << 20, 8 << 20, false},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		write(t, dir, "config.toml", fmt.Sprintf("logMaxBytesPerResource = %d\n", tt.in))
		l, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if l.Config.LogMaxBytesPerResource != tt.want || (len(l.Warnings) == 1) != tt.warn {
			t.Errorf("in=%d: got %d warnings=%q", tt.in, l.Config.LogMaxBytesPerResource, l.Warnings)
		}
	}
	dir := t.TempDir()
	write(t, dir, "config.toml", "logMaxBytesPerResource = -1\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("negative size accepted")
	}
}

func TestMouseWheelList(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     string
		warnWith []string // substrings of the one expected warning; nil means none
	}{
		{"no key", `theme = "mono"`, "cursor", nil},
		{"cursor", `mouseWheelList = "cursor"`, "cursor", nil},
		{"scroll", `mouseWheelList = "scroll"`, "scroll", nil},
		{"unknown", `mouseWheelList = "page"`, "cursor", []string{"mouseWheelList", `"page"`}},
		{"case sensitive", `mouseWheelList = "Scroll"`, "cursor", []string{"mouseWheelList", `"Scroll"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "config.toml", tt.body)
			l, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if l.Config.MouseWheelList != tt.want {
				t.Errorf("MouseWheelList = %q, want %q", l.Config.MouseWheelList, tt.want)
			}
			if tt.warnWith == nil {
				if len(l.Warnings) != 0 {
					t.Errorf("Warnings = %q, want none", l.Warnings)
				}
				return
			}
			if len(l.Warnings) != 1 {
				t.Fatalf("Warnings = %q, want exactly one", l.Warnings)
			}
			for _, s := range tt.warnWith {
				if !strings.Contains(l.Warnings[0], s) {
					t.Errorf("warning %q does not contain %q", l.Warnings[0], s)
				}
			}
		})
	}
}

func TestMouseWheelListJSON(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", `{"mouseWheelList": "scroll"}`)
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l.Config.MouseWheelList != "scroll" {
		t.Fatalf("MouseWheelList = %q, want scroll", l.Config.MouseWheelList)
	}
}
