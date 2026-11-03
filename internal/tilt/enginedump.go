package tilt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrUnknownFormat means a response decoded but didn't have the expected
// shape, e.g. an engine dump from an unsupported Tilt version.
var ErrUnknownFormat = errors.New("tilt: unrecognised response format")

// maxEngineDump bounds /api/dump/engine. The dump carries Tilt's whole
// log backlog, so it is far larger than other responses.
const maxEngineDump = 64 << 20

// engineDump is the part of Tilt's EngineState this package reads. The
// field names are Go's, because Tilt encodes EngineState without tags.
type engineDump struct {
	ManifestTargets map[string]struct {
		Manifest struct {
			Name                 string   `json:"Name"`
			ResourceDependencies []string `json:"ResourceDependencies"`
		} `json:"Manifest"`
	} `json:"ManifestTargets"`
}

// ResourceDependencies fetches /api/dump/engine through the guarded client
// (64 MiB limit) and returns each resource's resourceDependencies. The raw
// dump, which includes Tilt's log backlog, never leaves this package.
func (c *Client) ResourceDependencies(ctx context.Context) (map[string][]string, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	body, err := c.doLimit(ctx, http.MethodGet, "/api/dump/engine", nil, maxEngineDump)
	if err != nil {
		return nil, fmt.Errorf("engine dump: %w", err)
	}
	var d engineDump
	if err := json.Unmarshal(body, &d); err != nil {
		// Don't wrap err: a type error quotes the dump's field names.
		return nil, fmt.Errorf("engine dump: %w", ErrUnknownFormat)
	}
	if d.ManifestTargets == nil {
		return nil, fmt.Errorf("engine dump: %w: no ManifestTargets", ErrUnknownFormat)
	}
	out := make(map[string][]string, len(d.ManifestTargets))
	for key, mt := range d.ManifestTargets {
		name := mt.Manifest.Name
		if name == "" {
			name = key
		}
		out[name] = mt.Manifest.ResourceDependencies
	}
	return out, nil
}
