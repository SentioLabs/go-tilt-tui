package proc

import (
	"context"
	"time"
)

// --- Contract assertions ---

var (
	_ func(string) (Tilt, error)                                 = FindTilt
	_ func(Tilt, context.Context, int) ([]byte, error)           = Tilt.FileWatches
	_ func(Tilt, context.Context, int, []string) (*Child, error) = Tilt.Up
	_ func(*Child, time.Duration) error                          = (*Child).Stop
	_ func(*Child) <-chan struct{}                               = (*Child).Done
	_ func(*Child) error                                         = (*Child).ExitErr
	_ func(*Child) string                                        = (*Child).StderrTail
	_ func(context.Context, string) error                        = OpenURL
)
