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
	"strconv"
	"strings"
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
				return Tilt{}, fmt.Errorf("tiltBinaryPath %q: %w", override, noFileError{})
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

// noFileError is a missing override. It matches exec.ErrNotFound, but its
// text doesn't claim a $PATH search that never ran.
type noFileError struct{}

func (noFileError) Error() string { return "no such file" }

func (noFileError) Is(target error) bool { return target == exec.ErrNotFound }

// maxFileWatchOutput bounds `tilt get filewatches` output. More is an
// error, never a truncated list.
const maxFileWatchOutput = 16 << 20

// FileWatches runs `tilt get filewatches -o json --host localhost --port
// port` and returns its raw JSON. It never takes a hostname: Tilt's HTTP
// proxy refuses filewatches, so this is the one query that needs the CLI,
// and it only ever targets the local Tilt. Callers must not use it under
// --allow-remote.
func (t Tilt) FileWatches(ctx context.Context, port int) ([]byte, error) {
	if t.Binary == "" {
		return nil, errors.New("tilt get filewatches: no tilt binary (call FindTilt first)")
	}
	cmd := exec.CommandContext(ctx, t.Binary,
		"get", "filewatches", "-o", "json", "--host", "localhost", "--port", strconv.Itoa(port))
	stdout := &capped{max: maxFileWatchOutput}
	stderr := &tail{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if stdout.over {
		return nil, fmt.Errorf("tilt get filewatches: output over %d bytes", maxFileWatchOutput)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("tilt get filewatches: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("tilt get filewatches: %w", err)
	}
	return stdout.buf, nil
}

// capped keeps the first max bytes written to it and records whether
// more arrived. It never fails a write, so the writer never blocks.
type capped struct {
	max  int
	buf  []byte
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - len(c.buf); len(p) > room {
		c.buf = append(c.buf, p[:max(0, room)]...)
		c.over = true
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}
