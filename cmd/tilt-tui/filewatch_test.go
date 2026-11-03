package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
)

func TestFileWatchesNilUnderAllowRemote(t *testing.T) {
	called := false
	f := fileWatches(true, 10399, func() (proc.Tilt, error) { called = true; return proc.Tilt{}, nil })
	if f != nil {
		t.Fatal("want nil under --allow-remote")
	}
	if called {
		t.Fatal("resolved tilt under --allow-remote")
	}
}

func TestFileWatchesResolvesLazily(t *testing.T) {
	calls := 0
	f := fileWatches(false, 10399, func() (proc.Tilt, error) { calls++; return proc.Tilt{}, errors.New("no tilt") })
	if calls != 0 {
		t.Fatal("resolved tilt before first use")
	}
	if _, err := f(context.Background()); err == nil || !strings.Contains(err.Error(), "no tilt") {
		t.Fatalf("err = %v", err)
	}
}

// The subprocess gets localhost and the TUI's port, never --host.
func TestFileWatchesRunsLocalTilt(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\n" +
		`echo '{"kind":"List","items":[{"metadata":{"name":"local:api","annotations":{"tilt.dev/resource":"api"}},"spec":{"watchedPaths":["/w/src"]},"status":{}}]}'` + "\n"
	bin := filepath.Join(dir, "tilt")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TILT_HOST", "evil.example")
	f := fileWatches(false, 10399, func() (proc.Tilt, error) { return proc.Tilt{Binary: bin}, nil })
	ws, err := f(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].Resource != "api" || ws[0].WatchedPaths[0] != "/w/src" {
		t.Fatalf("ws = %+v", ws)
	}
	data, _ := os.ReadFile(argsFile)
	args := string(data)
	if !strings.Contains(args, "--host\nlocalhost\n") || !strings.Contains(args, "--port\n10399\n") || strings.Contains(args, "evil") {
		t.Fatalf("args = %q", args)
	}
}
