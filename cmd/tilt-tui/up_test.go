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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

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

func TestParseFlagsUsageListsSelfCommands(t *testing.T) {
	var out strings.Builder
	if _, err := parseFlags([]string{"-h"}, &out); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h: err = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"-port", "tilt-tui self update", "tilt-tui self channel"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("-h output %q lacks %q", out.String(), want)
		}
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
	_ = l.Close()
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
	t.Cleanup(func() { _ = f.sup.Close() })
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
	defer func() { _ = l.Close() }()
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
		_ = syscall.Kill(pid, syscall.SIGKILL)
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

// runMainEnv makes the test binary run main instead of the tests, so a
// test can run tilt-tui as a process and signal it.
const runMainEnv = "TILT_TUI_TEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// signalTilt writes a fake `tilt` to a new dir. It writes its PID to
// dir/pid, then runs until SIGTERM, which it records in dir/term.
func signalTilt(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\n" +
		"trap \"echo TERM > '" + filepath.Join(dir, "term") + "'; exit 0\" TERM\n" +
		"echo $$ > '" + filepath.Join(dir, "pid.tmp") + "'\n" +
		"mv '" + filepath.Join(dir, "pid.tmp") + "' '" + filepath.Join(dir, "pid") + "'\n" +
		"while :; do sleep 0.05; done\n"
	if err := os.WriteFile(filepath.Join(dir, "tilt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startTUI runs `tilt-tui --port port --up -- --file x` with the fake
// tilt in dir first on $PATH and its config in dir.
func startTUI(t *testing.T, dir string, port int, stdio []*os.File, sys *syscall.SysProcAttr) *os.Process {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// os.StartProcess keeps duplicate variables and the first one wins,
	// so drop the ones the test sets.
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k != "PATH" && k != "XDG_CONFIG_HOME" && k != "TILT_PORT" && k != runMainEnv {
			env = append(env, kv)
		}
	}
	env = append(env, runMainEnv+"=1",
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"XDG_CONFIG_HOME="+filepath.Join(dir, "cfg"))
	p, err := os.StartProcess(self,
		[]string{self, "--port", strconv.Itoa(port), "--up", "--", "--file", "x"},
		&os.ProcAttr{Env: env, Files: stdio, Sys: sys})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// waitPid waits for the fake tilt in dir to write its PID. If the test
// ends with that tilt still running, cleanup kills its process group.
func waitPid(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(dir, "pid"))
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Without a TERM record this tilt never exited, so the
				// PID is still its own.
				if _, err := os.Stat(filepath.Join(dir, "term")); err != nil && syscall.Kill(pid, 0) == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("fake tilt never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitExit waits for p to exit, killing it after d.
func waitExit(t *testing.T, p *os.Process, d time.Duration) *os.ProcessState {
	t.Helper()
	type result struct {
		st  *os.ProcessState
		err error
	}
	done := make(chan result, 1)
	go func() {
		st, err := p.Wait()
		done <- result{st, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.st
	case <-time.After(d):
		_ = p.Kill()
		<-done
		t.Fatalf("tilt-tui still running %v after the signal", d)
		return nil
	}
}

// checkTiltStopped fails unless the fake tilt in dir got SIGTERM and is
// gone.
func checkTiltStopped(t *testing.T, dir string, pid int) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "term")); err != nil {
		t.Errorf("fake tilt (pid %d) got no SIGTERM: %v", pid, err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("fake tilt (pid %d) still exists after tilt-tui exited: kill 0 = %v", pid, err)
	}
}

func TestUpSignalDuringStartupStopsTilt(t *testing.T) {
	dir := signalTilt(t)
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devNull.Close() }()
	// A new session has no terminal, so tilt-tui can't take over the one
	// running the tests.
	p := startTUI(t, dir, freePort(t), []*os.File{devNull, out, out}, &syscall.SysProcAttr{Setsid: true})
	pid := waitPid(t, dir)
	// tilt has just started, so tilt-tui is inside the 2 s startup window.
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	st := waitExit(t, p, 10*time.Second)
	checkTiltStopped(t, dir, pid)
	if st.ExitCode() != 1 {
		t.Errorf("tilt-tui ended with %v, want exit status 1", st)
	}
	if printed, _ := os.ReadFile(out.Name()); len(printed) != 0 {
		t.Errorf("tilt-tui printed %q, want nothing", printed)
	}
}

// Linux ioctl requests for the test pty, as numbered on amd64 and arm64.
const (
	linuxTIOCSPTLCK = 0x40045431
	linuxTIOCGPTN   = 0x80045430
	linuxTIOCSWINSZ = 0x5414
)

func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// openPTY opens a 100x30 pseudo-terminal pair.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	var unlock int32
	if err := ioctl(m, linuxTIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatalf("unlock pty: %v", err)
	}
	var n uint32
	if err := ioctl(m, linuxTIOCGPTN, unsafe.Pointer(&n)); err != nil {
		t.Fatalf("pty number: %v", err)
	}
	size := [4]uint16{30, 100, 0, 0} // rows, columns, unused pixel sizes
	if err := ioctl(m, linuxTIOCSWINSZ, unsafe.Pointer(&size)); err != nil {
		t.Fatalf("pty size: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

// lockedBuffer collects what tilt-tui draws on the pty.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Bubble Tea catches SIGINT and SIGTERM but not SIGHUP, which a closed
// terminal window sends.
func TestUpHangupQuitsAndStopsTilt(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("the test pty uses Linux ioctl numbers for amd64 and arm64")
	}
	dir := signalTilt(t)
	master, slave := openPTY(t)
	var drawn lockedBuffer
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		_, _ = io.Copy(&drawn, master) // ends with EIO once tilt-tui exits
	}()
	p := startTUI(t, dir, freePort(t), []*os.File{slave, slave, slave},
		&syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0})
	_ = slave.Close()
	pid := waitPid(t, dir)

	// The status line shows once launch has returned and the TUI runs.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(drawn.String(), "starting tilt") {
		if time.Now().After(deadline) {
			_ = p.Kill()
			t.Fatalf("the TUI never drew the starting state:\n%q", drawn.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := p.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	st := waitExit(t, p, 15*time.Second)
	checkTiltStopped(t, dir, pid)
	if !st.Exited() {
		t.Errorf("tilt-tui ended with %v, want a normal exit", st)
	}
	select {
	case <-copied:
	case <-time.After(5 * time.Second):
		t.Error("the pty stayed open after tilt-tui exited")
	}
}
