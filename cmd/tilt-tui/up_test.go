package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
)

// --- Contract assertions ---

var (
	_ child                                      = (*proc.Child)(nil)
	_ func(proc.Tilt) startFunc                  = tiltStarter
	_ func([]string, io.Writer) (options, error) = parseFlags
)

func TestParseFlags(t *testing.T) {
	t.Setenv("TILT_PORT", "10399")
	tests := []struct {
		name    string
		args    []string
		want    options
		wantErr string
	}{
		{name: "attach", args: nil,
			want: options{host: "localhost", port: 10399}},
		{name: "up without args", args: []string{"--up", "--port", "10400"},
			want: options{host: "localhost", port: 10400, up: true}},
		{name: "up with args", args: []string{"--up", "--", "--file", "Tiltfile.dev", "api", "--", "-x"},
			want: options{host: "localhost", port: 10399, up: true, tiltArgs: []string{"--file", "Tiltfile.dev", "api", "--", "-x"}}},
		{name: "flags after up", args: []string{"--up", "--port=10401", "--"},
			want: options{host: "localhost", port: 10401, up: true, tiltArgs: []string{}}},
		{name: "args without up", args: []string{"--", "--file", "x"}, wantErr: "arguments after -- need --up"},
		{name: "stray argument", args: []string{"--up", "api"}, wantErr: `unexpected argument "api": put tilt up arguments after --`},
		{name: "up and remote", args: []string{"--up", "--allow-remote"}, wantErr: "--up starts a local tilt: drop --allow-remote"},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "flag provided but not defined: -nope"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFlags(tc.args, io.Discard)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.host != tc.want.host || got.port != tc.want.port || got.up != tc.want.up ||
				got.allowRemote != tc.want.allowRemote || !slices.Equal(got.tiltArgs, tc.want.tiltArgs) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, err := parseFlags([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h: err = %v, want flag.ErrHelp", err)
	}
}

func TestCheckPortFree(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	err = checkPortFree("127.0.0.1", port)
	want := fmt.Sprintf("port %d in use; drop --up to attach", port)
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	l.Close()
	if err := checkPortFree("127.0.0.1", port); err != nil {
		t.Fatalf("closed port reported in use: %v", err)
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{codeErr(3), 3},
		{fmt.Errorf("tilt up: %w", codeErr(4)), 4},
		{errors.New("signal: killed"), -1},
	}
	for _, tc := range tests {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

// codeErr is an error with an exit code, like *exec.ExitError.
type codeErr int

func (c codeErr) Error() string { return "exit status " + strconv.Itoa(int(c)) }
func (c codeErr) ExitCode() int { return int(c) }

func TestWatchLoopStopAndRestart(t *testing.T) {
	var mu sync.Mutex
	running, runs := 0, 0
	started := make(chan struct{}, 10)
	w := &watchLoop{run: func(ctx context.Context) {
		mu.Lock()
		running++
		runs++
		mu.Unlock()
		started <- struct{}{}
		<-ctx.Done()
		mu.Lock()
		running--
		mu.Unlock()
	}}
	state := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return running, runs
	}

	w.Stop() // stopping a loop that never ran is a no-op
	w.Start(context.Background())
	w.Start(context.Background()) // already running: no second goroutine
	<-started
	if r, n := state(); r != 1 || n != 1 {
		t.Fatalf("after Start twice: running=%d runs=%d, want 1 and 1", r, n)
	}
	w.Stop()
	if r, _ := state(); r != 0 {
		t.Fatalf("Stop returned with %d loops running", r)
	}
	w.Stop()
	w.Start(context.Background())
	<-started
	if r, n := state(); r != 1 || n != 2 {
		t.Fatalf("after restart: running=%d runs=%d, want 1 and 2", r, n)
	}
	w.Stop()
}

// fakeChild is a child the test ends by closing done.
type fakeChild struct {
	done   chan struct{}
	err    error
	stderr string

	mu    sync.Mutex
	stops []time.Duration
}

func newFakeChild() *fakeChild { return &fakeChild{done: make(chan struct{})} }

func (c *fakeChild) Stop(grace time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops = append(c.stops, grace)
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}
func (c *fakeChild) Done() <-chan struct{} { return c.done }
func (c *fakeChild) ExitErr() error        { return c.err }
func (c *fakeChild) StderrTail() string    { return c.stderr }

// upCall is one start call the fake recorded.
type upCall struct {
	port int
	args []string
}

// fakeUp is a supervisor wired to fakes: each start call takes the next
// child or error, and every message sent lands in msgs.
type fakeUp struct {
	sup   *supervisor
	msgs  chan tea.Msg
	loops chan string // "start" and "stop" from the fake reconnect loop

	mu      sync.Mutex
	calls   []upCall
	results []any // *fakeChild or error, used in order
}

func newFakeUp(t *testing.T, port int, args []string, results ...any) *fakeUp {
	t.Helper()
	f := &fakeUp{msgs: make(chan tea.Msg, 20), loops: make(chan string, 20), results: results}
	f.sup = &supervisor{
		ctx:  context.Background(),
		port: port,
		args: args,
		send: func(m tea.Msg) { f.msgs <- m },
		watch: &watchLoop{run: func(ctx context.Context) {
			f.loops <- "start"
			<-ctx.Done()
			f.loops <- "stop"
		}},
		start: func(_ context.Context, port int, args []string) (child, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.calls = append(f.calls, upCall{port: port, args: args})
			r := f.results[0]
			f.results = f.results[1:]
			if err, ok := r.(error); ok {
				return nil, err
			}
			return r.(*fakeChild), nil
		},
	}
	t.Cleanup(func() { f.sup.Close() })
	return f
}

func (f *fakeUp) nextMsg(t *testing.T) tea.Msg {
	t.Helper()
	select {
	case m := <-f.msgs:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("no message sent")
		return nil
	}
}

func (f *fakeUp) nextLoop(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-f.loops:
		if got != want {
			t.Fatalf("reconnect loop: got %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("reconnect loop: no %q", want)
	}
}

func TestSupervisorReportsStartAndExit(t *testing.T) {
	c := newFakeChild()
	c.err, c.stderr = codeErr(1), "Error: Tiltfile failed\n"
	f := newFakeUp(t, 10399, []string{"--file", "Tiltfile.dev"}, c)

	if err := f.sup.launch(); err != nil {
		t.Fatal(err)
	}
	if got := f.calls; len(got) != 1 || got[0].port != 10399 || !slices.Equal(got[0].args, []string{"--file", "Tiltfile.dev"}) {
		t.Fatalf("start calls = %+v", got)
	}
	if m := f.nextMsg(t); m != (ui.ChildStartedMsg{}) {
		t.Fatalf("first message = %#v, want ChildStartedMsg", m)
	}
	f.nextLoop(t, "start")
	if err := f.sup.launch(); err != nil || len(f.calls) != 1 {
		t.Fatalf("launch with a running child started another: err=%v calls=%d", err, len(f.calls))
	}

	close(c.done) // tilt exits on its own
	f.nextLoop(t, "stop")
	m := f.nextMsg(t)
	if m != (ui.ChildExitedMsg{Code: 1, Stderr: "Error: Tiltfile failed\n"}) {
		t.Fatalf("exit message = %#v", m)
	}
	select {
	case got := <-f.loops:
		t.Fatalf("reconnect loop ran again after the exit: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSupervisorLaunchError(t *testing.T) {
	f := newFakeUp(t, 10399, nil, errors.New("tilt up exited: exit status 1\nError: no Tiltfile"))
	err := f.sup.launch()
	if err == nil || !strings.Contains(err.Error(), "Error: no Tiltfile") {
		t.Fatalf("err = %v, want tilt's stderr", err)
	}
	select {
	case m := <-f.msgs:
		t.Fatalf("a failed launch sent %#v", m)
	case got := <-f.loops:
		t.Fatalf("a failed launch ran the reconnect loop: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSupervisorCloseStopsChildAndLoop(t *testing.T) {
	c := newFakeChild()
	f := newFakeUp(t, 10399, nil, c)
	if err := f.sup.launch(); err != nil {
		t.Fatal(err)
	}
	f.nextMsg(t)
	f.nextLoop(t, "start")

	if err := f.sup.Close(); err != nil {
		t.Fatal(err)
	}
	f.nextLoop(t, "stop")
	c.mu.Lock()
	stops := slices.Clone(c.stops)
	c.mu.Unlock()
	if !slices.Equal(stops, []time.Duration{upGrace}) {
		t.Fatalf("Stop calls = %v, want one with %v", stops, upGrace)
	}
	select {
	case m := <-f.msgs:
		t.Fatalf("quit sent %#v", m)
	case <-time.After(50 * time.Millisecond):
	}
	if err := f.sup.launch(); err != nil || len(f.calls) != 1 {
		t.Fatalf("launch after Close started tilt: err=%v calls=%d", err, len(f.calls))
	}
}

// fakeTilt puts a `tilt` script on $PATH that records its arguments and
// its PID in dir, then runs body.
func fakeTilt(t *testing.T, body string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + filepath.Join(dir, "args") + "'\n" +
		"echo $$ > '" + filepath.Join(dir, "pid") + "'\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tilt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestUpLeavesNoTiltAfterQuit(t *testing.T) {
	dir := fakeTilt(t, "exec sleep 300")
	tl, err := proc.FindTilt("")
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	msgs := make(chan tea.Msg, 10)
	sup := &supervisor{
		ctx:   context.Background(),
		start: tiltStarter(tl),
		port:  port,
		args:  []string{"--file", "Tiltfile.dev"},
		send:  func(m tea.Msg) { msgs <- m },
		watch: &watchLoop{run: func(ctx context.Context) { <-ctx.Done() }},
	}
	if err := sup.launch(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("up\n--port\n%d\n--file\nTiltfile.dev\n", port); string(args) != want {
		t.Fatalf("tilt got args %q, want %q", args, want)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("fake tilt (pid %d) is not running: %v", pid, err)
	}

	if err := sup.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("fake tilt (pid %d) still exists after quit: kill 0 = %v", pid, err)
	}
	select {
	case m := <-msgs:
		if m != (ui.ChildStartedMsg{}) {
			t.Fatalf("unexpected message %#v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no ChildStartedMsg")
	}
	select {
	case m := <-msgs:
		t.Fatalf("quit sent %#v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUpEarlyExitReturnsStderr(t *testing.T) {
	fakeTilt(t, "echo 'Loading Tiltfile' >&2\n"+
		"printf 'Error: Tiltfile:3: undefined: local_resorce\\033]52;c;ZXZpbA==\\007\\033]0;pwned\\007\\n' >&2\n"+
		"exit 3")
	tl, err := proc.FindTilt("")
	if err != nil {
		t.Fatal(err)
	}
	sup := &supervisor{
		ctx:   context.Background(),
		start: tiltStarter(tl),
		port:  freePort(t),
		send:  func(m tea.Msg) { t.Errorf("an early exit sent %#v", m) },
		watch: &watchLoop{run: func(ctx context.Context) { <-ctx.Done() }},
	}
	err = sup.launch()
	if err == nil || !strings.Contains(err.Error(), "\x1b]52;") {
		t.Fatalf("err = %q, want tilt's raw stderr with the OSC 52 payload", err)
	}
	// main prints exactly this message.
	msg := safeStartError(err).Error()
	if strings.ContainsAny(msg, "\x1b\x07") {
		t.Fatalf("printed message holds an escape sequence: %q", msg)
	}
	for _, want := range []string{"Loading Tiltfile\n", "Error: Tiltfile:3: undefined: local_resorce"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("printed message %q lacks %q", msg, want)
		}
	}
}
