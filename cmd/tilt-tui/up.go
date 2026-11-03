package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
)

const (
	// upGrace is how long quit waits after SIGTERM before it sends SIGKILL.
	upGrace = 5 * time.Second
	// portCheckTimeout bounds the dial that tests whether the port is taken.
	portCheckTimeout = time.Second
)

// options are the parsed command line.
type options struct {
	host        string
	port        int
	allowRemote bool
	up          bool
	tiltArgs    []string // everything after "--", for `tilt up`
}

// parseFlags parses args (without the program name). Everything after
// the first "--" goes to `tilt up` and needs --up. Usage and flag errors
// go to out. -h returns flag.ErrHelp.
func parseFlags(args []string, out io.Writer) (options, error) {
	own, tiltArgs := args, []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		own, tiltArgs = args[:i], args[i+1:]
	}
	var o options
	fs := flag.NewFlagSet("tilt-tui", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.host, "host", tilt.DefaultHost, "Tilt API host")
	fs.IntVar(&o.port, "port", defaultPort(), "Tilt API port (default from $TILT_PORT, else 10350)")
	fs.BoolVar(&o.allowRemote, "allow-remote", false, "allow a non-loopback --host; Tilt's tokens then travel unencrypted")
	fs.BoolVar(&o.up, "up", false, "start `tilt up --port PORT ARGS...` with the ARGS after --, and stop it on quit")
	if err := fs.Parse(own); err != nil {
		return options{}, err
	}
	switch {
	case fs.NArg() > 0:
		return options{}, fmt.Errorf("unexpected argument %q: put tilt up arguments after --", fs.Arg(0))
	case len(tiltArgs) > 0 && !o.up:
		return options{}, errors.New("arguments after -- need --up")
	case o.up && o.allowRemote:
		return options{}, errors.New("--up starts a local tilt: drop --allow-remote")
	}
	o.tiltArgs = tiltArgs
	return o, nil
}

// checkPortFree returns an error when something already listens on
// host:port, because `tilt up` could not bind it.
func checkPortFree(host string, port int) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), portCheckTimeout)
	if err != nil {
		return nil
	}
	conn.Close()
	return fmt.Errorf("port %d in use; drop --up to attach", port)
}

// safeStartError returns err with each line of its text passed through
// termsafe.Text. Up's error holds tilt's raw stderr, and a Tiltfile
// command can write any escape sequence there. main prints the result.
func safeStartError(err error) error {
	lines := strings.Split(err.Error(), "\n")
	for i, l := range lines {
		lines[i] = termsafe.Text(l)
	}
	return errors.New(strings.Join(lines, "\n"))
}

// child is the part of *proc.Child the supervisor uses. Tests fake it.
type child interface {
	Stop(grace time.Duration) error
	Done() <-chan struct{}
	ExitErr() error
	StderrTail() string
}

// startFunc starts `tilt up --port port args...`. It has the shape of
// proc.Tilt.Up, so tests can replace it.
type startFunc func(ctx context.Context, port int, args []string) (child, error)

// tiltStarter adapts t.Up to a startFunc.
func tiltStarter(t proc.Tilt) startFunc {
	return func(ctx context.Context, port int, args []string) (child, error) {
		c, err := t.Up(ctx, port, args)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

// exitCode returns the exit code inside err: 0 for nil, and -1 when err
// carries none (a signal, or a start that failed).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}
	return -1
}

// watchLoop runs the reconnect loop in a goroutine. Stop ends it and
// Start begins it again. Stop waits for the goroutine, so nothing a
// stopped loop sends arrives after Stop returns.
type watchLoop struct {
	run func(ctx context.Context)

	mu     sync.Mutex
	cancel context.CancelFunc // nil while stopped
	done   chan struct{}
}

// Start runs w.run in a new goroutine unless it is already running.
func (w *watchLoop) Start(parent context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	go func() {
		defer close(done)
		w.run(ctx)
	}()
}

// Stop cancels the running loop and waits for it to return.
func (w *watchLoop) Stop() {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// supervisor owns the `tilt up` child under --up. It tells the UI when
// the child starts and exits, and it runs the reconnect loop only while
// a child runs.
type supervisor struct {
	ctx   context.Context // lives as long as the program
	start startFunc
	port  int
	args  []string
	send  func(tea.Msg) // p.Send
	watch *watchLoop

	mu        sync.Mutex
	child     child // the running child, nil once it exits
	launching bool  // a launch is waiting for start
	closed    bool
	launches  sync.WaitGroup // Close waits for launches in flight
}

// upOptions lets other files in package main add program options under
// --up. Each gets the supervisor. restart.go adds one.
var upOptions []func(*supervisor) tea.ProgramOption

// programOptions returns the options upOptions build for s, or none
// without --up.
func programOptions(s *supervisor) []tea.ProgramOption {
	if s == nil {
		return nil
	}
	var opts []tea.ProgramOption
	for _, f := range upOptions {
		opts = append(opts, f(s))
	}
	return opts
}

// launch starts a child with s.port and s.args. It returns start's
// error, which holds tilt's stderr when tilt exits within its startup
// window. It does nothing when a child runs, another launch is in
// flight, or Close was called.
func (s *supervisor) launch() error {
	s.mu.Lock()
	if s.closed || s.launching || s.child != nil {
		s.mu.Unlock()
		return nil
	}
	s.launching = true
	s.launches.Add(1)
	s.mu.Unlock()
	defer s.launches.Done()

	c, err := s.start(s.ctx, s.port, s.args)

	s.mu.Lock()
	s.launching = false
	closed := s.closed
	if err == nil && !closed {
		s.child = c
	}
	s.mu.Unlock()
	switch {
	case err != nil:
		return err
	case closed:
		return c.Stop(upGrace)
	}
	go s.supervise(c)
	return nil
}

// supervise reports c's start, runs the reconnect loop until c exits,
// then reports the exit. Sends block until the program runs, so it works
// in its own goroutine.
func (s *supervisor) supervise(c child) {
	s.send(ui.ChildStartedMsg{})
	s.mu.Lock()
	if !s.closed {
		s.watch.Start(s.ctx)
	}
	s.mu.Unlock()

	<-c.Done()

	s.mu.Lock()
	if s.child == c {
		s.child = nil
	}
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	s.watch.Stop()
	s.send(ui.ChildExitedMsg{Code: exitCode(c.ExitErr()), Stderr: c.StderrTail()})
}

// Close stops the reconnect loop, waits for a launch in flight and stops
// the child: SIGTERM, then SIGKILL after upGrace. main calls it on quit.
func (s *supervisor) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.watch.Stop()
	s.launches.Wait()

	s.mu.Lock()
	c := s.child
	s.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Stop(upGrace)
}
