package tilt

import (
	"context"
	"errors"
)

// ErrUnknownFormat means a response decoded but didn't have the expected
// shape, e.g. an engine dump from an unsupported Tilt version.
var ErrUnknownFormat = errors.New("tilt: unrecognised response format")

// ResourceDependencies fetches /api/dump/engine through the guarded client
// (64 MiB limit) and returns each resource's resourceDependencies. The raw
// dump, which includes Tilt's log backlog, never leaves this package.
func (c *Client) ResourceDependencies(ctx context.Context) (map[string][]string, error) {
	panic("T0 stub")
}
