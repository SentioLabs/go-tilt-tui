package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// --- Contract assertions ---

var (
	_ func(string) (Tilt, error)                                 = FindTilt
	_ func(Tilt, context.Context, int) ([]byte, error)           = Tilt.FileWatches
	_ func(Tilt, context.Context, int, []string) (*Child, error) = Tilt.Up
	_ func(*Child, time.Duration) error                          = (*Child).Stop
	_ func(*Child) <-chan struct{}                               = (*Child).Done
	_ func(*Child) error                                         = (*Child).ExitErr
	_ func(*Child) string                                        = (*Child).StderrTail
	_ func(context.Context, string) error                        = OpenURL
)

// --- Behavior tests ---

// testPort is never contacted: the fake tilt only records it. It is not
// 10350, where a real Tilt may be serving.
const testPort = 10399

// fakeTilt writes an executable sh script called tilt into a new temp dir
// and returns its path. The script body runs after "#!/bin/sh".
func fakeTilt(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tilt")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// shortStartup shrinks the startup window so tests don't wait 2 s each.
func shortStartup(t *testing.T) {
	t.Helper()
	old := startupWindow
	startupWindow = 300 * time.Millisecond
	t.Cleanup(func() { startupWindow = old })
}

// gone waits up to 3 s for pid to vanish or become a zombie. kill(pid, 0)
// succeeds on a zombie, and in a container whose PID 1 doesn't reap, a
// reparented grandchild stays one forever.
func gone(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) || zombie(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// zombie reports whether /proc shows pid in state Z. It is false where
// there is no /proc, such as macOS.
func zombie(pid int) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	// The line is "pid (comm) state ...". comm may hold ")", so the
	// state follows the last one.
	s := string(data)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(fields) > 0 && fields[0] == "Z"
}

// An exited child the test has not reaped is a zombie. gone counts it
// as gone, because a reparented grandchild can stay a zombie when PID 1
// doesn't reap.
func TestGoneSeesZombie(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("no /proc")
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	if !gone(cmd.Process.Pid) {
		t.Errorf("gone(zombie pid %d) = false", cmd.Process.Pid)
	}
}

// pidFromFile reads a pid the fake wrote, waiting for the file to appear.
func pidFromFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.HasSuffix(string(data), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("pid file %s: %v", path, err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s never appeared", path)
	return 0
}

func TestFindTiltOnPath(t *testing.T) {
	bin := fakeTilt(t, "exit 0")
	t.Setenv("PATH", filepath.Dir(bin))
	got, err := FindTilt("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Binary != bin {
		t.Errorf("Binary = %q, want %q", got.Binary, bin)
	}
}

func TestFindTiltOverride(t *testing.T) {
	bin := fakeTilt(t, "exit 0")
	t.Setenv("PATH", t.TempDir())
	got, err := FindTilt(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Binary != bin {
		t.Errorf("Binary = %q, want %q", got.Binary, bin)
	}
}

func TestFindTiltMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := FindTilt(""); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("FindTilt with no tilt on PATH = %v, want exec.ErrNotFound", err)
	}
	notExec := filepath.Join(t.TempDir(), "tilt")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FindTilt(notExec); err == nil || !strings.Contains(err.Error(), "tiltBinaryPath") {
		t.Errorf("FindTilt(non-executable) = %v, want a tiltBinaryPath error", err)
	}
}

// A missing override wraps exec.ErrNotFound like a missing tilt on PATH.
func TestFindTiltOverrideMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "tilt")
	_, err := FindTilt(missing)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("FindTilt(%q) = %v, want exec.ErrNotFound", missing, err)
	}
	if err == nil || !strings.Contains(err.Error(), "tiltBinaryPath") {
		t.Errorf("FindTilt(%q) = %v, want a tiltBinaryPath error", missing, err)
	}
	// An override with a slash is never searched on $PATH.
	if err != nil && strings.Contains(err.Error(), "$PATH") {
		t.Errorf("FindTilt(%q) = %v, want no mention of $PATH", missing, err)
	}
}

// A tilt in the current directory reached through "." on PATH is refused.
func TestFindTiltRefusesDotPath(t *testing.T) {
	bin := fakeTilt(t, "exit 0")
	t.Chdir(filepath.Dir(bin))
	t.Setenv("PATH", ".")
	if _, err := FindTilt(""); !errors.Is(err, exec.ErrDot) {
		t.Errorf("FindTilt via . on PATH = %v, want exec.ErrDot", err)
	}
}

func TestUpNeedsBinary(t *testing.T) {
	if _, err := (Tilt{}).Up(context.Background(), testPort, nil); err == nil {
		t.Fatal("Up with no binary = nil error")
	}
}

func TestUpFastExitReturnsStderr(t *testing.T) {
	shortStartup(t)
	bin := fakeTilt(t, `echo "Error: Tiltfile not found" >&2; exit 3`)
	start := time.Now()
	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err == nil {
		c.Stop(time.Second)
		t.Fatal("Up = nil error for a tilt that exits at once")
	}
	if c != nil {
		t.Error("Up returned a Child with its error")
	}
	if !strings.Contains(err.Error(), "Error: Tiltfile not found") {
		t.Errorf("error lacks stderr: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("error lacks exit status: %v", err)
	}
	if d := time.Since(start); d >= startupWindow {
		t.Errorf("Up took %v, want it to return as soon as tilt exits", d)
	}
}

// tilt starts a child in its group and then fails at startup. Up kills
// the child too.
func TestUpFastExitKillsGroup(t *testing.T) {
	shortStartup(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	bin := fakeTilt(t, `sleep 60 </dev/null >/dev/null 2>&1 &
echo $! > "`+pidFile+`"
exit 3`)
	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err == nil {
		c.Stop(time.Second)
		t.Fatal("Up = nil error for a tilt that exits at once")
	}
	if grandchild := pidFromFile(t, pidFile); !gone(grandchild) {
		syscall.Kill(grandchild, syscall.SIGKILL)
		t.Errorf("grandchild pid %d survived a failed Up", grandchild)
	}
}

func TestUpZeroExitDuringStartupIsAnError(t *testing.T) {
	shortStartup(t)
	bin := fakeTilt(t, "exit 0")
	if _, err := (Tilt{Binary: bin}).Up(context.Background(), testPort, nil); err == nil || !strings.Contains(err.Error(), "exit 0") {
		t.Fatalf("Up = %v, want an early-exit error", err)
	}
}

func TestUpPassesArgsAndStopsOnSIGTERM(t *testing.T) {
	shortStartup(t)
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bin := fakeTilt(t, `printf '%s\n' "$@" > "`+argsFile+`"
trap 'echo "got TERM" >&2; exit 0' TERM
echo "tilt starting" >&2
while :; do sleep 0.05; done`)

	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, []string{"--stream", "--", "a b"})
	if err != nil {
		t.Fatal(err)
	}
	pid := c.cmd.Process.Pid
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	wantArgs := []string{"up", "--port", "10399", "--stream", "--", "a b"}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Errorf("args = %q, want %q", gotArgs, wantArgs)
	}
	select {
	case <-c.Done():
		t.Fatal("Done closed while tilt runs")
	default:
	}

	start := time.Now()
	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("Stop took %v, want tilt to exit on SIGTERM", d)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Done still open after Stop")
	}
	if err := c.ExitErr(); err != nil {
		t.Errorf("ExitErr = %v, want nil after a clean SIGTERM exit", err)
	}
	if tail := c.StderrTail(); !strings.Contains(tail, "tilt starting") || !strings.Contains(tail, "got TERM") {
		t.Errorf("StderrTail = %q", tail)
	}
	if !gone(pid) {
		t.Errorf("tilt pid %d still exists after Stop", pid)
	}
	// A second Stop is a no-op.
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("second Stop = %v", err)
	}
}

func TestStopKillsTiltIgnoringSIGTERM(t *testing.T) {
	shortStartup(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild")
	// The grandchild inherits the ignored SIGTERM and tilt's process
	// group. Only SIGKILL to the group removes both.
	bin := fakeTilt(t, `trap '' TERM
sleep 60 &
echo $! > "`+pidFile+`"
while :; do sleep 0.05; done`)

	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	pid := c.cmd.Process.Pid
	grandchild := pidFromFile(t, pidFile)

	grace := 300 * time.Millisecond
	start := time.Now()
	if err := c.Stop(grace); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < grace {
		t.Errorf("Stop returned after %v, before the %v grace ran out", d, grace)
	}
	var ee *exec.ExitError
	if err := c.ExitErr(); !errors.As(err, &ee) || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Errorf("ExitErr = %v, want killed by SIGKILL", err)
	}
	if !gone(pid) {
		t.Errorf("tilt pid %d still exists after Stop", pid)
	}
	if !gone(grandchild) {
		t.Errorf("grandchild pid %d still exists after Stop", grandchild)
	}
}

// tilt exits on SIGTERM but leaves a child that ignores it. Stop still
// clears the group.
func TestStopKillsLeftoverGroupMembers(t *testing.T) {
	shortStartup(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild")
	bin := fakeTilt(t, `(trap '' TERM; exec sleep 60) </dev/null >/dev/null 2>&1 &
echo $! > "`+pidFile+`"
trap 'exit 0' TERM
while :; do sleep 0.05; done`)

	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	grandchild := pidFromFile(t, pidFile)
	if err := c.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !gone(c.cmd.Process.Pid) {
		t.Error("tilt still exists after Stop")
	}
	if !gone(grandchild) {
		t.Errorf("grandchild pid %d survived Stop", grandchild)
	}
}

func TestDoneClosesWhenTiltExitsLater(t *testing.T) {
	shortStartup(t)
	bin := fakeTilt(t, `sleep 0.6; echo "Tiltfile failed" >&2; exit 1`)
	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		c.Stop(time.Second)
		t.Fatal("Done never closed")
	}
	var ee *exec.ExitError
	if err := c.ExitErr(); !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Errorf("ExitErr = %v, want exit status 1", err)
	}
	if !strings.Contains(c.StderrTail(), "Tiltfile failed") {
		t.Errorf("StderrTail = %q", c.StderrTail())
	}
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop after exit = %v", err)
	}
}

// tilt exits on its own after startup and leaves a child in its group.
// The child is killed before Done closes, while tilt's pid can't have
// been reused. Stop then has nothing left to do.
func TestStopAfterExitKillsGroup(t *testing.T) {
	shortStartup(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	bin := fakeTilt(t, `sleep 60 </dev/null >/dev/null 2>&1 &
echo $! > "`+pidFile+`"
sleep 0.6
exit 0`)
	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	grandchild := pidFromFile(t, pidFile)
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		c.Stop(time.Second)
		t.Fatal("Done never closed")
	}
	if !gone(grandchild) {
		syscall.Kill(grandchild, syscall.SIGKILL)
		t.Errorf("grandchild pid %d survived tilt's exit", grandchild)
	}
	if err := c.Stop(time.Second); err != nil {
		t.Errorf("Stop after exit = %v", err)
	}
}

// tilt exits 0 on SIGTERM, but a child that ignores SIGTERM still holds
// tilt's stderr. Wait gives up after waitDelay. That is still a clean
// exit, so ExitErr is nil.
func TestStopCleanExitWithHeldStderr(t *testing.T) {
	shortStartup(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild")
	bin := fakeTilt(t, `(trap '' TERM; exec sleep 60) &
echo $! > "`+pidFile+`"
trap 'exit 0' TERM
while :; do sleep 0.05; done`)
	c, err := Tilt{Binary: bin}.Up(context.Background(), testPort, nil)
	if err != nil {
		t.Fatal(err)
	}
	grandchild := pidFromFile(t, pidFile)
	// tilt exits on SIGTERM, so Stop must not wait out the grace.
	grace := 10 * time.Second
	start := time.Now()
	if err := c.Stop(grace); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d >= 5*time.Second {
		t.Errorf("Stop took %v, want about waitDelay and well under the %v grace", d, grace)
	}
	if err := c.ExitErr(); err != nil {
		t.Errorf("ExitErr = %v, want nil for a tilt that exited 0", err)
	}
	if !gone(grandchild) {
		syscall.Kill(grandchild, syscall.SIGKILL)
		t.Errorf("grandchild pid %d survived Stop", grandchild)
	}
}

func TestUpContextCancelStopsTilt(t *testing.T) {
	old := startupWindow
	startupWindow = 10 * time.Second
	t.Cleanup(func() { startupWindow = old })
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	bin := fakeTilt(t, `echo $$ > "`+pidFile+`"
while :; do sleep 0.05; done`)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel once the script runs. t.Fatal can't be called off the test
	// goroutine, so this polls without t.
	go func() {
		defer cancel()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if _, err := os.Stat(pidFile); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if _, err := (Tilt{Binary: bin}).Up(ctx, testPort, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Up = %v, want context.Canceled", err)
	}
	if pid := pidFromFile(t, pidFile); !gone(pid) {
		t.Errorf("tilt pid %d survived a cancelled Up", pid)
	}
}

func TestTailKeepsLast64KiB(t *testing.T) {
	tl := &tail{max: stderrMax}
	line := strings.Repeat("x", 1000) + "\n"
	for range 200 {
		tl.Write([]byte(line))
	}
	tl.Write([]byte("last line\n"))
	got := tl.String()
	if len(got) != stderrMax {
		t.Errorf("len = %d, want %d", len(got), stderrMax)
	}
	if !strings.HasSuffix(got, "last line\n") {
		t.Errorf("tail lost the newest bytes: %q", got[len(got)-20:])
	}
	if cap(tl.buf) > 4*stderrMax {
		t.Errorf("buffer grew to %d bytes", cap(tl.buf))
	}
}

func TestTailOneBigWrite(t *testing.T) {
	tl := &tail{max: 8}
	if n, _ := tl.Write([]byte("0123456789abcdef")); n != 16 {
		t.Errorf("Write returned %d, want 16", n)
	}
	if got := tl.String(); got != "89abcdef" {
		t.Errorf("String = %q", got)
	}
}

func TestTailDropsSplitRune(t *testing.T) {
	tl := &tail{max: 4}
	tl.Write([]byte("aé€")) // a, é (2 bytes), € (3 bytes)
	// The last 4 bytes start inside é. Its continuation byte is dropped.
	if got := tl.String(); got != "€" {
		t.Errorf("String = %q, want %q", got, "€")
	}
}
