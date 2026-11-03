package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
)

// exitFirstChild launches f's first child, lets it exit and drains the
// messages and loop events that produces.
func exitFirstChild(t *testing.T, f *fakeUp, c *fakeChild) {
	t.Helper()
	if err := f.sup.launch(); err != nil {
		t.Fatal(err)
	}
	f.nextMsg(t)
	f.nextLoop(t, "start")
	close(c.done)
	f.nextLoop(t, "stop")
	if m := f.nextMsg(t); m != (ui.ChildExitedMsg{Code: 0}) {
		t.Fatalf("exit message = %#v", m)
	}
}

func TestRestartFilterRestartsWithOriginalArgs(t *testing.T) {
	first, second := newFakeChild(), newFakeChild()
	args := []string{"--file", "Tiltfile.dev", "api"}
	f := newFakeUp(t, 10399, args, first, second)
	exitFirstChild(t, f, first)

	filter := restartFilter(f.sup)
	if got := filter(nil, ui.RestartRequestMsg{}); got != nil {
		t.Fatalf("filter passed the restart request on: %#v", got)
	}
	if m := f.nextMsg(t); m != (ui.ChildStartedMsg{}) {
		t.Fatalf("message after restart = %#v, want ChildStartedMsg", m)
	}
	f.nextLoop(t, "start")

	f.mu.Lock()
	calls := slices.Clone(f.calls)
	f.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("start called %d times, want 2", len(calls))
	}
	for i, c := range calls {
		if c.port != 10399 || !slices.Equal(c.args, args) {
			t.Fatalf("start call %d = %+v, want port 10399 and %q", i, c, args)
		}
	}
}

func TestRestartFilterPassesOtherMessages(t *testing.T) {
	f := newFakeUp(t, 10399, nil)
	filter := restartFilter(f.sup)
	for _, msg := range []tea.Msg{ui.ConnMsg{Connecting: true}, ui.ChildStartedMsg{}, tea.KeyPressMsg{Code: 'R', Text: "R"}} {
		if got := filter(nil, msg); got != msg {
			t.Fatalf("filter(%#v) = %#v, want it unchanged", msg, got)
		}
	}
}

func TestRestartWhileChildRunsDoesNothing(t *testing.T) {
	c := newFakeChild()
	f := newFakeUp(t, 10399, nil, c)
	if err := f.sup.launch(); err != nil {
		t.Fatal(err)
	}
	f.nextMsg(t)
	f.sup.restart()
	f.mu.Lock()
	n := len(f.calls)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("restart with a running child called start again (%d calls)", n)
	}
	select {
	case m := <-f.msgs:
		t.Fatalf("restart with a running child sent %#v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRestartFailureReportsExit(t *testing.T) {
	first := newFakeChild()
	f := newFakeUp(t, 10399, nil, first, errors.New("tilt up exited: exit status 1\nError: port 10399 taken"))
	exitFirstChild(t, f, first)

	f.sup.restart()
	m, ok := f.nextMsg(t).(ui.ChildExitedMsg)
	if !ok || m.Code != -1 || !strings.Contains(m.Stderr, "Error: port 10399 taken") {
		t.Fatalf("message after a failed restart = %#v", m)
	}
	select {
	case got := <-f.loops:
		t.Fatalf("a failed restart ran the reconnect loop: %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUpOptionsIncludeRestartFilter(t *testing.T) {
	if got := len(programOptions(nil)); got != 0 {
		t.Fatalf("attach mode got %d program options, want 0", got)
	}
	if got := len(programOptions(&supervisor{})); got != 1 {
		t.Fatalf("--up got %d program options, want 1", got)
	}
}

// waitingSupervisor is a supervisor on ctx whose start waits for its own
// ctx to end, like tilt inside its startup window. Each start's ctx
// arrives on the returned channel.
func waitingSupervisor(ctx context.Context) (*supervisor, chan context.Context) {
	starts := make(chan context.Context, 1)
	s := &supervisor{
		ctx:   ctx,
		send:  func(tea.Msg) {},
		watch: &watchLoop{run: func(ctx context.Context) { <-ctx.Done() }},
		start: func(ctx context.Context, _ int, _ []string) (child, error) {
			starts <- ctx
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	return s, starts
}

func TestCloseCancelsRestartInFlight(t *testing.T) {
	s, starts := waitingSupervisor(context.Background())
	restarted := make(chan struct{})
	go func() {
		defer close(restarted)
		s.restart()
	}()
	select {
	case <-starts:
	case <-time.After(2 * time.Second):
		t.Fatal("restart never called start")
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close waited for the launch in flight instead of cancelling it")
	}
	<-restarted
}

func TestRestartEndsWithSupervisorContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, starts := waitingSupervisor(ctx)
	msgs := make(chan tea.Msg, 1)
	s.send = func(m tea.Msg) { msgs <- m }
	go s.restart()
	select {
	case <-starts:
	case <-time.After(2 * time.Second):
		t.Fatal("restart never called start")
	}

	cancel() // a signal ends the supervisor's ctx
	select {
	case m := <-msgs:
		if m != (ui.ChildExitedMsg{Code: -1, Stderr: context.Canceled.Error()}) {
			t.Fatalf("message after the signal = %#v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("the restart ignored the end of the supervisor's ctx")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseDuringRestartStopsNewTilt(t *testing.T) {
	dir := signalTilt(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	tl, err := proc.FindTilt("")
	if err != nil {
		t.Fatal(err)
	}
	s := &supervisor{
		ctx:   context.Background(),
		start: tiltStarter(tl),
		port:  freePort(t),
		send:  func(tea.Msg) {},
		watch: &watchLoop{run: func(ctx context.Context) { <-ctx.Done() }},
	}
	restarted := make(chan struct{})
	go func() {
		defer close(restarted)
		s.restart()
	}()
	pid := waitPid(t, dir) // tilt is inside its 2 s startup window

	begin := time.Now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Up's in-window stop kills after 1 s, and the startup window is 2 s.
	if d := time.Since(begin); d >= 1500*time.Millisecond {
		t.Errorf("Close took %v: it waited out the startup window", d)
	}
	checkTiltStopped(t, dir, pid)
	<-restarted
}
