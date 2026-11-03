package tilt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ErrConflict means the object changed on the server since it was read.
var ErrConflict = errors.New("tilt: object changed since it was read")

// clickTimeFormat is RFC 3339 with exactly six fractional digits. Tilt's
// API server rejects lastClickedAt in any other precision.
const clickTimeFormat = "2006-01-02T15:04:05.000000Z07:00"

// now is the clock ClickButton stamps lastClickedAt with. Tests fix it.
var now = time.Now

// buttonStatusPath is the UIButton status subresource behind Tilt's API
// proxy. tilt-tui PUTs its clicks to the same path.
func buttonStatusPath(name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return "", fmt.Errorf("click button: invalid name %q", name)
	}
	return "/proxy/apis/tilt.dev/v1alpha1/uibuttons/" + url.PathEscape(name) + "/status", nil
}

// clickPayload is the body of a click. It carries the button's metadata,
// including resourceVersion, and its status with the new click. tilt-tui
// sends the same two fields and no spec.
type clickPayload struct {
	Metadata ObjectMeta   `json:"metadata"`
	Status   ButtonStatus `json:"status"`
}

// ClickButton records a click on b with the given input values and
// returns the updated button (with its new resourceVersion). Inputs not in
// values keep their current value. Returns ErrConflict on HTTP 409.
//
// A text, choice or hidden input takes a string and a bool input takes a
// bool. "Current value" means the spec's: the text and bool default, the
// first choice and the hidden value. Tilt rewrites the hidden value itself,
// as the disable toggle does with its "action" input. A key that names no
// input, a value of the wrong type or a choice not on the list is an error,
// and nothing is sent.
func (c *Client) ClickButton(ctx context.Context, b Button, values map[string]any) (Button, error) {
	name := b.Metadata.Name
	path, err := buttonStatusPath(name)
	if err != nil {
		return Button{}, err
	}
	inputs, err := clickInputs(b.Spec.Inputs, values)
	if err != nil {
		return Button{}, fmt.Errorf("click %s: %w", name, err)
	}
	status := b.Status
	status.LastClickedAt = now().UTC().Format(clickTimeFormat)
	status.Inputs = inputs
	body, err := json.Marshal(clickPayload{Metadata: b.Metadata, Status: status})
	if err != nil {
		return Button{}, err
	}

	if err := c.ensureSession(ctx); err != nil {
		return Button{}, err
	}
	out, err := c.do(ctx, http.MethodPut, path, body)
	if err != nil {
		var se *statusError
		if errors.As(err, &se) && se.code == http.StatusConflict {
			return Button{}, fmt.Errorf("click %s: %w", name, ErrConflict)
		}
		return Button{}, fmt.Errorf("click %s: %w", name, err)
	}
	var got Button
	if err := json.Unmarshal(out, &got); err != nil {
		return Button{}, fmt.Errorf("click %s: decode response: %w", name, err)
	}
	return got, nil
}

// clickInputs merges values over each input's current value, in spec
// order. Every input gets a status, as tilt-tui sends them.
func clickInputs(specs []InputSpec, values map[string]any) ([]InputStatus, error) {
	known := make(map[string]bool, len(specs))
	out := make([]InputStatus, 0, len(specs))
	for _, s := range specs {
		known[s.Name] = true
		v, set := values[s.Name]
		st := InputStatus{Name: s.Name}
		switch {
		case s.Text != nil:
			val, err := stringValue(s.Name, v, set, s.Text.DefaultValue)
			if err != nil {
				return nil, err
			}
			st.Text = &StringValue{Value: val}
		case s.Bool != nil:
			val := s.Bool.DefaultValue
			if set {
				bv, ok := v.(bool)
				if !ok {
					return nil, fmt.Errorf("input %q takes a bool, got %T", s.Name, v)
				}
				val = bv
			}
			st.Bool = &BoolValue{Value: val}
		case s.Hidden != nil:
			val, err := stringValue(s.Name, v, set, s.Hidden.Value)
			if err != nil {
				return nil, err
			}
			st.Hidden = &StringValue{Value: val}
		case s.Choice != nil:
			def := ""
			if len(s.Choice.Choices) > 0 {
				def = s.Choice.Choices[0]
			}
			val, err := stringValue(s.Name, v, set, def)
			if err != nil {
				return nil, err
			}
			if len(s.Choice.Choices) > 0 && !slices.Contains(s.Choice.Choices, val) {
				return nil, fmt.Errorf("input %q has no choice %q", s.Name, val)
			}
			st.Choice = &StringValue{Value: val}
		default:
			if set {
				return nil, fmt.Errorf("input %q has no known type", s.Name)
			}
		}
		out = append(out, st)
	}
	for k := range values {
		if !known[k] {
			return nil, fmt.Errorf("button has no input %q", k)
		}
	}
	return out, nil
}

func stringValue(name string, v any, set bool, def string) (string, error) {
	if !set {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("input %q takes a string, got %T", name, v)
	}
	return s, nil
}
