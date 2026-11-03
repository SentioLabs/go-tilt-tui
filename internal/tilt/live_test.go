//go:build live

// The live tests run against a real Tilt started from the repository
// root with:
//
//	tilt up --port 10399 -f testdata/tiltfile/Tiltfile.buttons
//
// Run them with:
//
//	go test -tags live -count=1 -run Live ./internal/tilt/
//
// TILT_LIVE_PORT picks another port. Port 10350 is refused, because the
// developer's own Tilt runs there.
package tilt

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	liveDefaultPort = 10399
	liveForbidden   = DefaultPort // 10350
	liveWait        = 30 * time.Second
	liveResource    = "buttons"
	liveForm        = "live-form"
	liveConfirm     = "live-confirm"
)

// liveTilt is one /ws/view connection for one test. It merges every
// message into a Store and keeps every log segment's text.
type liveTilt struct {
	client *Client

	mu      sync.Mutex
	store   *Store
	logs    strings.Builder
	err     error // set when Watch returns
	changed chan struct{}
}

func livePort(t *testing.T) int {
	t.Helper()
	port := liveDefaultPort
	if s := os.Getenv("TILT_LIVE_PORT"); s != "" {
		p, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("TILT_LIVE_PORT=%q: %v", s, err)
		}
		port = p
	}
	if port == liveForbidden {
		t.Fatalf("refusing port %d: the developer's Tilt runs there", liveForbidden)
	}
	return port
}

// liveConnect starts watching Tilt and waits for the first snapshot.
func liveConnect(t *testing.T) *liveTilt {
	t.Helper()
	c, err := New(DefaultHost, livePort(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	lt := &liveTilt{client: c, store: NewStore(), changed: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := c.Watch(ctx, func(v View) {
			lt.mu.Lock()
			lt.store.Apply(v)
			if v.LogList != nil {
				for _, s := range v.LogList.Segments {
					lt.logs.WriteString(s.Text)
				}
			}
			lt.mu.Unlock()
			lt.notify()
		})
		lt.mu.Lock()
		lt.err = err
		lt.mu.Unlock()
		lt.notify()
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	lt.waitFor(t, "the first snapshot with the live-form button", func(s *Store, _ string) bool {
		_, ok := s.Button(liveForm)
		return ok
	})
	return lt
}

func (lt *liveTilt) notify() {
	select {
	case lt.changed <- struct{}{}:
	default:
	}
}

// waitFor polls cond after every message until it holds or liveWait
// passes.
func (lt *liveTilt) waitFor(t *testing.T, what string, cond func(s *Store, logs string) bool) {
	t.Helper()
	deadline := time.After(liveWait)
	for {
		lt.mu.Lock()
		ok, err := cond(lt.store, lt.logs.String()), lt.err
		lt.mu.Unlock()
		if ok {
			return
		}
		if err != nil {
			t.Fatalf("watch ended while waiting for %s: %v (is tilt up --port %d running?)", what, err, livePort(t))
		}
		select {
		case <-lt.changed:
		case <-time.After(250 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out after %s waiting for %s", liveWait, what)
		}
	}
}

func (lt *liveTilt) button(t *testing.T, name string) Button {
	t.Helper()
	lt.mu.Lock()
	defer lt.mu.Unlock()
	b, ok := lt.store.Button(name)
	if !ok {
		t.Fatalf("Tilt has no button %q", name)
	}
	return b
}

func (lt *liveTilt) resource(name string) (Resource, bool) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	for _, r := range lt.store.Resources() {
		if r.Name() == name {
			return r, true
		}
	}
	return Resource{}, false
}

// statusValue returns the value Tilt recorded for input name, and false
// when the status has none.
func statusValue(b Button, name string) (any, bool) {
	for _, in := range b.Status.Inputs {
		if in.Name != name {
			continue
		}
		switch {
		case in.Text != nil:
			return in.Text.Value, true
		case in.Bool != nil:
			return in.Bool.Value, true
		case in.Choice != nil:
			return in.Choice.Value, true
		case in.Hidden != nil:
			return in.Hidden.Value, true
		}
	}
	return nil, false
}

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), liveWait)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveTrigger(t *testing.T) {
	lt := liveConnect(t)
	lt.waitFor(t, "the first build of "+liveResource, func(s *Store, _ string) bool {
		r, ok := lt.resourceLocked(s, liveResource)
		return ok && len(r.Status.BuildHistory) > 0 && r.Status.BuildHistory[0].FinishTime != nil
	})
	before := time.Now()
	if err := lt.client.Trigger(liveCtx(t), liveResource); err != nil {
		t.Fatal(err)
	}
	lt.waitFor(t, "a build that started after the trigger", func(s *Store, _ string) bool {
		r, ok := lt.resourceLocked(s, liveResource)
		if !ok || len(r.Status.BuildHistory) == 0 {
			return false
		}
		st := r.Status.BuildHistory[0].StartTime
		return st != nil && st.After(before.Add(-time.Second))
	})
}

// resourceLocked finds a resource in s. waitFor already holds the lock.
func (lt *liveTilt) resourceLocked(s *Store, name string) (Resource, bool) {
	for _, r := range s.Resources() {
		if r.Name() == name {
			return r, true
		}
	}
	return Resource{}, false
}

func TestLiveClickButtonEachInput(t *testing.T) {
	lt := liveConnect(t)
	cases := []struct {
		name  string
		input string
		value any
	}{
		{"text", "msg", "typed in the live test"},
		{"bool", "force", true},
		{"choice", "env", "prod"},
		{"hidden", "token", "s3cret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := lt.button(t, liveForm)
			got, err := lt.client.ClickButton(liveCtx(t), b, map[string]any{tc.input: tc.value})
			if err != nil {
				t.Fatalf("ClickButton: %v", err)
			}
			if got.Metadata.ResourceVersion == "" || got.Metadata.ResourceVersion == b.Metadata.ResourceVersion {
				t.Fatalf("returned resourceVersion %q, sent %q", got.Metadata.ResourceVersion, b.Metadata.ResourceVersion)
			}
			if v, ok := statusValue(got, tc.input); !ok || v != tc.value {
				t.Fatalf("returned status %s = %v (%v), want %v", tc.input, v, ok, tc.value)
			}
			lt.waitFor(t, "/ws/view to show "+tc.input+" = "+strconv.Quote(toString(tc.value)), func(s *Store, _ string) bool {
				cur, ok := s.Button(liveForm)
				if !ok || cur.Status.LastClickedAt == b.Status.LastClickedAt {
					return false
				}
				v, ok := statusValue(cur, tc.input)
				return ok && v == tc.value
			})
		})
	}
}

func TestLiveClickButtonAllInputs(t *testing.T) {
	lt := liveConnect(t)
	b := lt.button(t, liveForm)
	want := map[string]any{"msg": "all at once", "force": false, "env": "staging", "token": "s3cret"}
	if _, err := lt.client.ClickButton(liveCtx(t), b, want); err != nil {
		t.Fatalf("ClickButton: %v", err)
	}
	lt.waitFor(t, "/ws/view to show every value", func(s *Store, _ string) bool {
		cur, ok := s.Button(liveForm)
		if !ok || cur.Status.LastClickedAt == b.Status.LastClickedAt {
			return false
		}
		for name, w := range want {
			if v, ok := statusValue(cur, name); !ok || v != w {
				return false
			}
		}
		return true
	})
	// The click starts btn-live-form, which echoes the values it got.
	lt.waitFor(t, "the btn-live-form log line", func(_ *Store, logs string) bool {
		return strings.Contains(logs, "live-form msg=all at once force=false env=staging token=s3cret")
	})
}

func TestLiveDisableToggle(t *testing.T) {
	lt := liveConnect(t)
	lt.waitFor(t, "the disable toggle of "+liveResource, func(s *Store, _ string) bool {
		_, ok := s.DisableButton(liveResource)
		return ok
	})
	click := func(t *testing.T) string {
		t.Helper()
		lt.mu.Lock()
		b, _ := lt.store.DisableButton(liveResource)
		lt.mu.Unlock()
		action := ""
		values := map[string]any{}
		for _, in := range b.Spec.Inputs {
			if in.Hidden != nil {
				values[in.Name] = in.Hidden.Value
				if in.Name == "action" {
					action = in.Hidden.Value
				}
			}
		}
		if action == "" {
			t.Fatalf("toggle %s has no hidden action input: %+v", b.Metadata.Name, b.Spec.Inputs)
		}
		if _, err := lt.client.ClickButton(liveCtx(t), b, values); err != nil {
			t.Fatalf("ClickButton(%s): %v", b.Metadata.Name, err)
		}
		return action
	}
	actionFlipped := func(sent string) func(*Store, string) bool {
		return func(s *Store, _ string) bool {
			b, ok := s.DisableButton(liveResource)
			if !ok {
				return false
			}
			for _, in := range b.Spec.Inputs {
				if in.Name == "action" && in.Hidden != nil {
					return in.Hidden.Value != sent
				}
			}
			return false
		}
	}
	isDisabled := func(want bool) func(*Store, string) bool {
		return func(s *Store, _ string) bool {
			r, ok := lt.resourceLocked(s, liveResource)
			return ok && r.IsDisabled() == want
		}
	}

	if r, ok := lt.resource(liveResource); !ok || r.IsDisabled() {
		t.Fatalf("%s must start enabled (found %v)", liveResource, ok)
	}
	t.Cleanup(func() {
		// Leave Tilt as the test found it.
		if r, ok := lt.resource(liveResource); ok && r.IsDisabled() {
			click(t)
		}
	})

	sent := click(t)
	lt.waitFor(t, liveResource+" disabled", isDisabled(true))
	lt.waitFor(t, "the toggle's action to change from "+sent, actionFlipped(sent))

	sent = click(t)
	lt.waitFor(t, liveResource+" enabled", isDisabled(false))
	lt.waitFor(t, "the toggle's action to change from "+sent, actionFlipped(sent))
}

func TestLiveConflict(t *testing.T) {
	lt := liveConnect(t)
	stale := lt.button(t, liveConfirm)
	fresh, err := lt.client.ClickButton(liveCtx(t), stale, nil)
	if err != nil {
		t.Fatalf("first click: %v", err)
	}
	if fresh.Metadata.ResourceVersion == stale.Metadata.ResourceVersion {
		t.Fatalf("resourceVersion did not change: %s", fresh.Metadata.ResourceVersion)
	}
	_, err = lt.client.ClickButton(liveCtx(t), stale, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("click with stale resourceVersion %s: err = %v, want ErrConflict", stale.Metadata.ResourceVersion, err)
	}
	if _, err := lt.client.ClickButton(liveCtx(t), fresh, nil); err != nil {
		t.Fatalf("click with the returned button: %v", err)
	}
}

func toString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}
