package main

import (
	"context"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// fileWatches returns ui.Deps.FileWatches. It is nil under
// --allow-remote: tilt get would need the user's host, and no
// subprocess ever gets it. The panel then says it needs a local Tilt.
//
// find runs on first use, so attach mode starts even when tilt isn't
// installed. Each call asks the tilt on localhost at port.
func fileWatches(allowRemote bool, port int, find func() (proc.Tilt, error)) func(context.Context) ([]tilt.FileWatch, error) {
	if allowRemote {
		return nil
	}
	return func(ctx context.Context) ([]tilt.FileWatch, error) {
		t, err := find()
		if err != nil {
			return nil, err
		}
		raw, err := t.FileWatches(ctx, port)
		if err != nil {
			return nil, err
		}
		return tilt.ParseFileWatches(raw)
	}
}
