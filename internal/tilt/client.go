package tilt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	DefaultHost = "localhost"
	DefaultPort = 10350

	// sessionCookie is set by Tilt (v0.37+) on GET / and is required on
	// every API and WebSocket request.
	sessionCookie = "Tilt-Token"

	// maxMessageBytes bounds a single /ws/view message. The first snapshot
	// carries the whole log backlog, so the websocket library's 32 KiB
	// default is far too small. 64 MiB is generous but still bounded.
	maxMessageBytes = 64 << 20

	requestTimeout = 10 * time.Second
)

// buildReasonCLI is model.BuildReasonFlagTriggerCLI in Tilt. tilt-tui sends
// the same value so the Tilt UI labels the build as a manual trigger.
const buildReasonCLI = 16

// Client talks to one Tilt HUD server. It never shells out and never
// contacts anything other than the configured host:port.
type Client struct {
	base *url.URL
	http *http.Client
}

// New returns a client for the Tilt server at host:port.
func New(host string, port int) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	base := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	return &Client{
		base: base,
		// No client-wide Timeout: it would also cap the long-lived
		// WebSocket. Each plain request gets its own context deadline.
		http: &http.Client{Jar: jar},
	}, nil
}

// Addr returns host:port for display.
func (c *Client) Addr() string { return c.base.Host }

// ensureSession loads the Tilt-Token cookie into the jar if it isn't there.
func (c *Client) ensureSession(ctx context.Context) error {
	if c.hasSession() {
		return nil
	}
	if _, err := c.do(ctx, http.MethodGet, "/", nil); err != nil {
		return fmt.Errorf("open tilt session: %w", err)
	}
	if !c.hasSession() {
		return errors.New("tilt did not set a Tilt-Token cookie (is this a Tilt server?)")
	}
	return nil
}

func (c *Client) hasSession() bool {
	for _, ck := range c.http.Jar.Cookies(c.base) {
		if ck.Name == sessionCookie {
			return true
		}
	}
	return false
}

// do runs one bounded HTTP request and returns the body (capped at 1 MiB).
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Watch connects to /ws/view and calls fn for every message until ctx is
// cancelled or the connection drops. It returns nil only when ctx ends.
// fn runs on the reading goroutine and must not block for long.
func (c *Client) Watch(ctx context.Context, fn func(View)) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	token, err := c.do(ctx, http.MethodGet, "/api/websocket_token", nil)
	if err != nil {
		return fmt.Errorf("get websocket token: %w", err)
	}

	wsURL := *c.base
	wsURL.Scheme = "ws"
	wsURL.Path = "/ws/view"
	wsURL.RawQuery = url.Values{"csrf": {strings.TrimSpace(string(token))}}.Encode()

	dialCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	conn, _, err := websocket.Dial(dialCtx, wsURL.String(), &websocket.DialOptions{HTTPClient: c.http})
	cancel()
	if err != nil {
		return fmt.Errorf("connect %s: %w", wsURL.Redacted(), err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxMessageBytes)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read view: %w", err)
		}
		var v View
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("decode view: %w", err)
		}
		fn(v)
	}
}

// Trigger asks Tilt to rebuild one resource, like clicking its trigger
// button in the web UI.
func (c *Client) Trigger(ctx context.Context, name string) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"manifest_names": []string{name},
		"build_reason":   buildReasonCLI,
	})
	if err != nil {
		return err
	}
	if _, err := c.do(ctx, http.MethodPost, "/api/trigger", body); err != nil {
		return fmt.Errorf("trigger %s: %w", name, err)
	}
	return nil
}
