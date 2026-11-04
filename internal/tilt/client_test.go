package tilt

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	fakeToken = "cookie-secret"
	fakeCSRF  = "csrf-secret"
)

// fakeTilt mimics the auth flow of a Tilt v0.37+ HUD server: GET / sets the
// Tilt-Token cookie, and every other endpoint rejects requests without it.
type fakeTilt struct {
	t        *testing.T
	messages []string // raw JSON sent on /ws/view, in order
	triggers chan map[string]any
}

func (f *fakeTilt) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: fakeToken, Path: "/"})
		_, _ = io.WriteString(w, "<html></html>")
	})
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ck, err := r.Cookie(sessionCookie)
			if err != nil || ck.Value != fakeToken {
				http.Error(w, "missing session", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/websocket_token", authed(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, fakeCSRF)
	}))
	mux.HandleFunc("GET /ws/view", authed(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("csrf") != fakeCSRF {
			http.Error(w, "bad csrf", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			f.t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for _, m := range f.messages {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(m)); err != nil {
				return
			}
		}
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	mux.HandleFunc("POST /api/trigger", authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.triggers <- body
	}))
	return mux
}

func newFake(t *testing.T, messages ...string) (*fakeTilt, *Client) {
	t.Helper()
	f := &fakeTilt{t: t, messages: messages, triggers: make(chan map[string]any, 1)}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	c, err := New(host, port, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func TestWatchAuthenticatesAndStreams(t *testing.T) {
	_, c := newFake(t,
		`{"isComplete":true,"uiResources":[{"metadata":{"name":"api"},"status":{"order":2,"updateStatus":"ok","runtimeStatus":"ok"}}]}`,
		`{"uiResources":[{"metadata":{"name":"web"},"status":{"order":3,"updateStatus":"in_progress"}}]}`,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []View
	err := c.Watch(ctx, func(v View) { got = append(got, v) })
	// The fake closes after sending, so Watch must report the drop.
	if err == nil {
		t.Fatal("Watch returned nil after server closed; want an error")
	}
	if len(got) != 2 {
		t.Fatalf("got %d views, want 2", len(got))
	}
	if !got[0].IsComplete || got[0].UIResources[0].Name() != "api" {
		t.Errorf("first view = %+v", got[0])
	}
	if got[1].UIResources[0].Status.UpdateStatus != UpdateInProgress {
		t.Errorf("second view status = %q", got[1].UIResources[0].Status.UpdateStatus)
	}
}

func TestWatchHandlesLargeSnapshot(t *testing.T) {
	// Bigger than the websocket library's 32 KiB default read limit.
	big := strings.Repeat("x", 256<<10)
	_, c := newFake(t, `{"isComplete":true,"logList":{"segments":[{"text":"`+big+`"}]}}`)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int
	_ = c.Watch(ctx, func(v View) { n = len(v.LogList.Segments[0].Text) })
	if n != len(big) {
		t.Fatalf("decoded %d bytes of log text, want %d", n, len(big))
	}
}

func TestTriggerSendsManifestName(t *testing.T) {
	f, c := newFake(t)
	if err := c.Trigger(context.Background(), "api"); err != nil {
		t.Fatal(err)
	}
	body := <-f.triggers
	names, _ := body["manifest_names"].([]any)
	if len(names) != 1 || names[0] != "api" {
		t.Errorf("manifest_names = %v", body["manifest_names"])
	}
	if body["build_reason"] != float64(buildReasonCLI) {
		t.Errorf("build_reason = %v", body["build_reason"])
	}
}

func TestDoRefusesOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, defaultMaxBody+1))
	}))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, err := New(host, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.do(context.Background(), http.MethodGet, "/big", nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("do = %v, want ErrTooLarge", err)
	}
	if out, err := c.doLimit(context.Background(), http.MethodGet, "/big", nil, defaultMaxBody+1); err != nil || len(out) != defaultMaxBody+1 {
		t.Fatalf("doLimit at exact size: len=%d err=%v", len(out), err)
	}
}

// --- Contract assertions ---

var (
	_ func(string, int, Options) (*Client, error)                            = New
	_ func(*Client, context.Context) error                                   = (*Client).CheckHost
	_ func(*Client, context.Context) (map[string][]string, error)            = (*Client).ResourceDependencies
	_ func(*Client, context.Context, Button, map[string]any) (Button, error) = (*Client).ClickButton
)
