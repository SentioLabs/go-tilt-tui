package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlagsVersion(t *testing.T) {
	o, err := parseFlags([]string{"--version"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !o.version {
		t.Fatal("--version did not set options.version")
	}
	o, err = parseFlags(nil, io.Discard)
	if err != nil || o.version {
		t.Fatalf("default: version=%v err=%v", o.version, err)
	}
}

func TestVersionFlagSkipsConfigAndTilt(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "tilt-tui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "tilt-tui", "config.toml"), []byte("this is = = not toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{runMainEnv + "=1", "PATH=", "XDG_CONFIG_HOME=" + cfg, "TILT_PORT=10399", "HOME=" + t.TempDir()}
	p, err := os.StartProcess(self, []string{self, "--version"},
		&os.ProcAttr{Env: env, Files: []*os.File{nil, w, w}})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	out, _ := io.ReadAll(r)
	st, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Success() {
		t.Fatalf("exit %d, output %q", st.ExitCode(), out)
	}
	if !strings.HasPrefix(string(out), "tilt-tui ") || !strings.HasSuffix(string(out), ")\n") {
		t.Fatalf("output %q, want \"tilt-tui <version> (<commit>, <date>)\\n\"", out)
	}
}
