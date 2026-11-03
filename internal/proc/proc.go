// Package proc is the only package that starts processes. Arguments are
// always passed as a list, never through a shell.
package proc

import "context"

// Tilt runs the tilt binary resolved once at startup.
type Tilt struct {
	Binary string
}

// FindTilt resolves override (from config) or "tilt" on $PATH.
func FindTilt(override string) (Tilt, error) { panic("T0 stub") }

// FileWatches runs `tilt get filewatches -o json --host localhost --port
// port` and returns its raw JSON. It never takes a hostname: Tilt's HTTP
// proxy refuses filewatches, so this is the one query that needs the CLI,
// and it only ever targets the local Tilt. Callers must not use it under
// --allow-remote.
func (t Tilt) FileWatches(ctx context.Context, port int) ([]byte, error) {
	panic("T0 stub")
}
