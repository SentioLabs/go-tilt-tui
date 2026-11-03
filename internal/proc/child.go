package proc

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Values the --up flow depends on. Tests shorten startupWindow.
var (
	// startupWindow is how long Up watches for an early exit.
	startupWindow = 2 * time.Second
	// stderrMax is how much of tilt's stderr Child keeps.
	stderrMax = 64 << 10
)

// waitDelay bounds how long Wait waits for stderr to close after tilt
// exits. A grandchild that inherited stderr would otherwise hold Done
// open forever.
const waitDelay = 2 * time.Second

// Up starts `tilt up --port port args...`. It returns an error, including
// tilt's stderr, if tilt exits within the startup window.
//
// ctx bounds the startup window only. Cancelling it during the window
// stops tilt and returns ctx.Err(). After Up returns, only Stop ends the
// process. tilt runs in its own process group with stdin and stdout on
// /dev/null.
func (t Tilt) Up(ctx context.Context, port int, args []string) (*Child, error) {
	if t.Binary == "" {
		return nil, errors.New("tilt up: no tilt binary (call FindTilt first)")
	}
	argv := append([]string{"up", "--port", strconv.Itoa(port)}, args...)
	cmd := exec.Command(t.Binary, argv...)
	c := &Child{cmd: cmd, done: make(chan struct{}), stderr: &tail{max: stderrMax}}
	cmd.Stderr = c.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tilt up: %w", err)
	}
	go func() {
		err := cmd.Wait()
		if errors.Is(err, exec.ErrWaitDelay) {
			// tilt exited 0 but a grandchild held stderr open past
			// waitDelay. That is still a clean exit.
			err = nil
		}
		// Kill anything tilt left in its group. Any member still holding
		// stderr is alive now, and the kernel doesn't reuse tilt's pid
		// while its group has members. Once the group empties the pid is
		// free, so a later kill could hit an unrelated group. signalGroup
		// already treats ESRCH as success. EPERM means only a member that
		// changed user is left, and nothing here can kill it.
		_ = signalGroup(cmd.Process.Pid, syscall.SIGKILL)
		c.mu.Lock()
		c.exitErr = err
		c.mu.Unlock()
		close(c.done)
	}()

	timer := time.NewTimer(startupWindow)
	defer timer.Stop()
	select {
	case <-timer.C:
		return c, nil
	case <-c.done:
		return nil, c.startupError()
	case <-ctx.Done():
		return nil, errors.Join(ctx.Err(), c.Stop(time.Second))
	}
}

// Child is a running `tilt up`.
type Child struct {
	cmd    *exec.Cmd
	done   chan struct{}
	stderr *tail

	mu      sync.Mutex
	exitErr error
}

func (c *Child) startupError() error {
	how := "exit 0"
	if err := c.ExitErr(); err != nil {
		how = err.Error()
	}
	msg := fmt.Sprintf("tilt up exited within %s (%s)", startupWindow, how)
	if s := c.StderrTail(); s != "" {
		msg += ":\n" + s
	}
	return errors.New(msg)
}

// Stop sends SIGTERM, waits up to grace, then sends SIGKILL.
//
// The signals go to tilt's whole process group, so children tilt started
// in that group stop too. Stop returns once tilt has exited. It does
// nothing if tilt has already exited.
func (c *Child) Stop(grace time.Duration) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	pgid := c.cmd.Process.Pid
	if err := signalGroup(pgid, syscall.SIGTERM); err != nil {
		return err
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		if err := signalGroup(pgid, syscall.SIGKILL); err != nil {
			return err
		}
		<-c.done
	}
	// tilt is gone. Kill anything it left in its group, such as a child
	// that ignored SIGTERM.
	return signalGroup(pgid, syscall.SIGKILL)
}

// signalGroup signals every process in group pgid. A group that is
// already empty is not an error.
func signalGroup(pgid int, sig syscall.Signal) error {
	if err := syscall.Kill(-pgid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal tilt (pgid %d): %w", pgid, err)
	}
	return nil
}

// Done is closed when the process exits for any reason. By then anything
// tilt left in its process group has been sent SIGKILL.
func (c *Child) Done() <-chan struct{} { return c.done }

// ExitErr returns how the process ended. Call it after Done is closed.
// It is nil for exit code 0 and an *exec.ExitError otherwise.
func (c *Child) ExitErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

// StderrTail returns the last 64 KiB of the process's stderr.
func (c *Child) StderrTail() string { return c.stderr.String() }

// tail keeps the last max bytes written to it. It is safe for concurrent
// use: exec copies stderr into it while the UI reads it.
type tail struct {
	mu        sync.Mutex
	max       int
	buf       []byte
	truncated bool
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(p)
	if len(p) >= t.max {
		p = p[len(p)-t.max:]
		t.buf = append(t.buf[:0], p...)
		t.truncated = true
		return n, nil
	}
	t.buf = append(t.buf, p...)
	// Compact only past twice the limit, so each byte is copied a
	// bounded number of times.
	if len(t.buf) > 2*t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
		t.truncated = true
	}
	return n, nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
		t.truncated = true
	}
	if t.truncated {
		// The cut may land inside a rune. Drop its continuation bytes.
		for len(b) > 0 && !utf8.RuneStart(b[0]) {
			b = b[1:]
		}
	}
	return string(b)
}
