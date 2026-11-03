// Package proc is the only package that starts processes. Arguments are
// always passed as a list, never through a shell.
package proc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
)

// Tilt runs the tilt binary resolved once at startup.
type Tilt struct {
	Binary string
}

// FindTilt resolves override (from config) or "tilt" on $PATH.
//
// An override containing a slash must name an executable file. A bare
// name is looked up on $PATH like "tilt". Binary is the absolute path
// found, so later calls never search $PATH again.
func FindTilt(override string) (Tilt, error) {
	name := override
	if name == "" {
		name = "tilt"
	}
	path, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		if override != "" {
			// LookPath reports a missing slash path as fs.ErrNotExist.
			// Callers test for exec.ErrNotFound either way.
			if errors.Is(err, fs.ErrNotExist) {
				return Tilt{}, fmt.Errorf("tiltBinaryPath %q: %w: %w", override, exec.ErrNotFound, err)
			}
			return Tilt{}, fmt.Errorf("tiltBinaryPath %q: %w", override, err)
		}
		return Tilt{}, fmt.Errorf("find tilt: %w (install tilt or set tiltBinaryPath)", err)
	}
	if err != nil {
		// exec.ErrDot: $PATH named the current directory. Running a
		// binary found there is how a cloned repo could plant one.
		return Tilt{}, fmt.Errorf("find tilt: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Tilt{}, fmt.Errorf("find tilt: %w", err)
	}
	return Tilt{Binary: abs}, nil
}

// FileWatches runs `tilt get filewatches -o json --host localhost --port
// port` and returns its raw JSON. It never takes a hostname: Tilt's HTTP
// proxy refuses filewatches, so this is the one query that needs the CLI,
// and it only ever targets the local Tilt. Callers must not use it under
// --allow-remote.
func (t Tilt) FileWatches(ctx context.Context, port int) ([]byte, error) {
	panic("T0 stub")
}
