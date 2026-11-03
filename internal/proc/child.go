package proc

import (
	"context"
	"time"
)

// Up starts `tilt up --port port args...`. It returns an error, including
// tilt's stderr, if tilt exits within the startup window.
func (t Tilt) Up(ctx context.Context, port int, args []string) (*Child, error) {
	panic("T0 stub")
}

// Child is a running `tilt up`.
type Child struct{}

// Stop sends SIGTERM, waits up to grace, then sends SIGKILL.
func (c *Child) Stop(grace time.Duration) error { panic("T0 stub") }

// Done is closed when the process exits for any reason.
func (c *Child) Done() <-chan struct{} { panic("T0 stub") }

// ExitErr returns how the process ended. Call it after Done is closed.
func (c *Child) ExitErr() error { panic("T0 stub") }

// StderrTail returns the last 64 KiB of the process's stderr.
func (c *Child) StderrTail() string { panic("T0 stub") }
