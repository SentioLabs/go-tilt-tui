package tilt

import (
	"context"
	"errors"
)

// ErrConflict means the object changed on the server since it was read.
var ErrConflict = errors.New("tilt: object changed since it was read")

// ClickButton records a click on b with the given input values and
// returns the updated button (with its new resourceVersion). Inputs not in
// values keep their current value. Returns ErrConflict on HTTP 409.
func (c *Client) ClickButton(ctx context.Context, b Button, values map[string]any) (Button, error) {
	panic("T0 stub")
}
