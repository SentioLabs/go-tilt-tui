package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sentiolabs/selfupdate-go"

	"github.com/sentiolabs/go-tilt-tui/internal/update"
)

// --- Contract assertions ---

var _ func(context.Context, *selfupdate.Updater, []string, io.Writer) error = runSelf

var _ func(context.Context, time.Duration, func(int), io.Writer) (context.Context, func()) = interruptible

// --- Helpers ---

// fakeSource lists one stable release, latest, and one rc release. It
// returns ctx's error once ctx ends.
type fakeSource struct{ latest, rc string }

func (f fakeSource) Latest(ctx context.Context) (selfupdate.Release, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.Release{}, err
	}
	return selfupdate.Release{Tag: f.latest}, nil
}

func (f fakeSource) List(ctx context.Context, _ int) ([]selfupdate.Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []selfupdate.Release{{Tag: f.rc, Prerelease: true}, {Tag: f.latest}}, nil
}

// fakeInstaller counts Prepare and Commit calls and never touches a file.
type fakeInstaller struct {
	prepared, committed int
	tags                []string
}

func (f *fakeInstaller) Prepare(_ context.Context, rel selfupdate.Release) (selfupdate.Staged, error) {
	f.prepared++
	f.tags = append(f.tags, rel.Tag)
	return fakeStaged{f}, nil
}

type fakeStaged struct{ f *fakeInstaller }

func (s fakeStaged) Commit(context.Context) error {
	s.f.committed++
	return nil
}

func (fakeStaged) Close() error { return nil }

// selfFixture is an updater on version with fake parts. out collects
// both the updater's output and runSelf's.
type selfFixture struct {
	u     *selfupdate.Updater
	inst  *fakeInstaller
	store *selfupdate.MemStore
	out   *bytes.Buffer
}

func newSelfFixture(version string) *selfFixture {
	f := &selfFixture{inst: &fakeInstaller{}, store: &selfupdate.MemStore{}, out: &bytes.Buffer{}}
	f.u = &selfupdate.Updater{
		Name:      "tilt-tui",
		Version:   version,
		Source:    fakeSource{latest: "v0.5.0", rc: "v0.6.0-rc.1"},
		Store:     f.store,
		Installer: f.inst,
		Out:       f.out,
		ErrOut:    f.out,
		In:        strings.NewReader(""),
	}
	return f
}

func (f *selfFixture) run(t *testing.T, args ...string) error {
	t.Helper()
	return runSelf(context.Background(), f.u, args, f.out)
}

// --- Behavior tests ---

func TestSelfUpdateCheckInstallsNothing(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--check"},
		{"update", "--check", "-y"},
		{"update", "--check", "--force", "--yes"},
	} {
		f := newSelfFixture("v0.4.0")
		if err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if !strings.Contains(f.out.String(), "Update available: v0.4.0 -> v0.5.0 (stable channel)") {
			t.Errorf("%q: output %q lacks the check line", args, f.out)
		}
		if f.inst.prepared != 0 || f.inst.committed != 0 {
			t.Errorf("%q: installer prepared %d, committed %d, want 0", args, f.inst.prepared, f.inst.committed)
		}
	}
}

func TestSelfUpdateInstalls(t *testing.T) {
	for _, args := range [][]string{
		{"update", "-y"},
		{"update", "--yes"},
		{"update", "-yes"},
	} {
		f := newSelfFixture("v0.4.0")
		if err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if f.inst.prepared != 1 || f.inst.committed != 1 {
			t.Errorf("%q: installer prepared %d, committed %d, want 1 and 1", args, f.inst.prepared, f.inst.committed)
		}
		if len(f.inst.tags) != 1 || f.inst.tags[0] != "v0.5.0" {
			t.Errorf("%q: installed %q, want v0.5.0", args, f.inst.tags)
		}
	}
}

// Without -y the updater asks, and an empty stdin declines.
func TestSelfUpdatePromptsWithoutYes(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	if err := f.run(t, "update"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "Continue? [y/N]") || !strings.Contains(f.out.String(), "Update cancelled.") {
		t.Errorf("output %q, want a declined prompt", f.out)
	}
	if f.inst.prepared != 0 {
		t.Errorf("installer prepared %d times, want 0", f.inst.prepared)
	}
}

func TestSelfUpdateForceReinstalls(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--force", "-y"},
		{"update", "-f", "-y"},
		{"update", "-y", "-force"},
	} {
		f := newSelfFixture("v0.5.0")
		if err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if f.inst.prepared != 1 || f.inst.committed != 1 {
			t.Errorf("%q: installer prepared %d, committed %d, want 1 and 1", args, f.inst.prepared, f.inst.committed)
		}
	}
}

func TestSelfUpdateCurrentWithoutForce(t *testing.T) {
	f := newSelfFixture("v0.5.0")
	if err := f.run(t, "update", "-y"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "is up to date") {
		t.Errorf("output %q lacks the up to date line", f.out)
	}
	if f.inst.prepared != 0 {
		t.Errorf("installer prepared %d times, want 0", f.inst.prepared)
	}
}

func TestSelfUpdateRejectsArgument(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	err := f.run(t, "update", "extra")
	if err == nil || !strings.Contains(err.Error(), `"extra"`) {
		t.Fatalf("err = %v, want one naming \"extra\"", err)
	}
	if f.inst.prepared != 0 {
		t.Errorf("installer prepared %d times, want 0", f.inst.prepared)
	}
}

func TestSelfUpdateCancelledContext(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runSelf(ctx, f.u, []string{"update", "-y"}, f.out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if f.inst.prepared != 0 {
		t.Errorf("installer prepared %d times, want 0", f.inst.prepared)
	}
}

func TestSelfUpdateHelp(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	if err := f.run(t, "update", "-h"); err != nil {
		t.Fatalf("-h: %v", err)
	}
	for _, want := range []string{"tilt-tui self update --check", "-check", "-force", "-yes"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("help %q lacks %q", f.out, want)
		}
	}
	if f.inst.prepared != 0 {
		t.Errorf("installer prepared %d times, want 0", f.inst.prepared)
	}
}

func TestSelfChannelShowAndSwitch(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	if err := f.run(t, "channel"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "Current update channel: stable") {
		t.Fatalf("output %q, want stable", f.out)
	}
	f.out.Reset()
	if err := f.run(t, "channel", "rc", "-y"); err != nil {
		t.Fatal(err)
	}
	if f.store.Current != selfupdate.ChannelRC {
		t.Fatalf("store = %q, want rc", f.store.Current)
	}
	f.out.Reset()
	if err := f.run(t, "channel"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "Current update channel: rc") {
		t.Fatalf("output %q, want rc", f.out)
	}
}

// A flag may come before the channel name.
func TestSelfChannelFlagFirst(t *testing.T) {
	for _, args := range [][]string{
		{"channel", "-y", "nightly"},
		{"channel", "--yes", "nightly"},
		{"channel", "nightly", "--yes"},
	} {
		f := newSelfFixture("v0.4.0")
		if err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if f.store.Current != selfupdate.ChannelNightly {
			t.Errorf("%q: store = %q, want nightly", args, f.store.Current)
		}
	}
}

// Without -y a switch to rc asks, and an empty stdin declines.
func TestSelfChannelPromptsWithoutYes(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	if err := f.run(t, "channel", "rc"); err != nil {
		t.Fatal(err)
	}
	if f.store.Current != "" {
		t.Fatalf("store = %q, want unchanged", f.store.Current)
	}
	if !strings.Contains(f.out.String(), "Cancelled.") {
		t.Errorf("output %q, want a declined prompt", f.out)
	}
}

func TestSelfChannelInvalid(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	err := f.run(t, "channel", "bogus", "-y")
	if err == nil || !strings.Contains(err.Error(), `invalid channel "bogus"`) {
		t.Fatalf("err = %v, want the library's invalid channel error", err)
	}
	if f.store.Current != "" {
		t.Fatalf("store = %q, want unchanged", f.store.Current)
	}
}

func TestSelfChannelTooManyArgs(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	err := f.run(t, "channel", "rc", "nightly")
	if err == nil || !strings.Contains(err.Error(), `"nightly"`) {
		t.Fatalf("err = %v, want one naming \"nightly\"", err)
	}
	if f.store.Current != "" {
		t.Fatalf("store = %q, want unchanged", f.store.Current)
	}
}

func TestSelfChannelHelp(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	if err := f.run(t, "channel", "-h"); err != nil {
		t.Fatalf("-h: %v", err)
	}
	for _, want := range []string{"tilt-tui self channel rc -y", "nightly", "-yes"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("help %q lacks %q", f.out, want)
		}
	}
}

func TestSelfUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}, {"help"}} {
		f := newSelfFixture("v0.4.0")
		if err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		for _, want := range []string{"update", "channel"} {
			if !strings.Contains(f.out.String(), want) {
				t.Errorf("%q: usage %q lacks %q", args, f.out, want)
			}
		}
	}
}

func TestSelfUnknownCommand(t *testing.T) {
	f := newSelfFixture("v0.4.0")
	err := f.run(t, "frob")
	if err == nil || !strings.Contains(err.Error(), `"frob"`) {
		t.Fatalf("err = %v, want one naming \"frob\"", err)
	}
	if !strings.Contains(f.out.String(), "update") || !strings.Contains(f.out.String(), "channel") {
		t.Errorf("output %q, want usage", f.out)
	}
}

// runTiltTUI runs the test binary as tilt-tui with args, its config in
// cfg and an empty PATH, so no gh or tilt can run. It returns stdout
// and stderr together.
func runTiltTUI(t *testing.T, cfg string, args ...string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{runMainEnv + "=1", "PATH=", "XDG_CONFIG_HOME=" + cfg, "TILT_PORT=10399", "HOME=" + t.TempDir()}
	p, err := os.StartProcess(self, append([]string{self}, args...),
		&os.ProcAttr{Env: env, Files: []*os.File{nil, w, w}})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	out, _ := io.ReadAll(r)
	st, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Success() {
		t.Fatalf("%q: exit %d, output %q", args, st.ExitCode(), out)
	}
	return string(out)
}

// main sends `self` to runSelf before parseFlags, which would reject the
// argument. The channel lands in selfupdate.toml in the config dir.
func TestMainSelfChannel(t *testing.T) {
	cfg := t.TempDir()
	if out := runTiltTUI(t, cfg, "self", "channel"); !strings.Contains(out, "Current update channel: stable") {
		t.Fatalf("self channel: output %q", out)
	}
	runTiltTUI(t, cfg, "self", "channel", "rc", "-y")
	if _, err := os.Stat(filepath.Join(cfg, "tilt-tui", update.ChannelFile)); err != nil {
		t.Fatalf("self channel rc -y left no %s: %v", update.ChannelFile, err)
	}
	if out := runTiltTUI(t, cfg, "self", "channel"); !strings.Contains(out, "Current update channel: rc") {
		t.Fatalf("self channel after rc: output %q", out)
	}
	if out := runTiltTUI(t, cfg, "self"); !strings.Contains(out, "update") {
		t.Fatalf("self: output %q, want usage", out)
	}
}

// watchFixture runs watchSignals on a test channel. exits gets each code
// passed to exit.
type watchFixture struct {
	sig    chan os.Signal
	exits  chan int
	errOut *bytes.Buffer
	ctx    context.Context
	stop   func()
}

func newWatchFixture(t *testing.T, grace time.Duration) *watchFixture {
	t.Helper()
	f := &watchFixture{sig: make(chan os.Signal, 2), exits: make(chan int, 2), errOut: &bytes.Buffer{}}
	f.ctx, f.stop = watchSignals(context.Background(), f.sig, grace, func(code int) { f.exits <- code }, f.errOut)
	return f
}

// waitDone fails unless the fixture's ctx ends within a second.
func (f *watchFixture) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-f.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ctx not cancelled by the signal")
	}
}

// waitExit returns the code passed to exit, failing after d.
func (f *watchFixture) waitExit(t *testing.T, d time.Duration) int {
	t.Helper()
	select {
	case code := <-f.exits:
		return code
	case <-time.After(d):
		t.Fatalf("exit not called within %v", d)
		return 0
	}
}

func TestWatchSignalsCancelsOnSignal(t *testing.T) {
	f := newWatchFixture(t, time.Hour)
	if f.ctx.Err() != nil {
		t.Fatal("ctx cancelled before any signal")
	}
	f.sig <- syscall.SIGINT
	f.waitDone(t)
	f.stop()
	select {
	case code := <-f.exits:
		t.Fatalf("exit(%d) called, want none", code)
	default:
	}
}

// A command still running grace after the signal is cut off.
func TestWatchSignalsExitsAfterGrace(t *testing.T) {
	for _, tc := range []struct {
		sig  os.Signal
		want int
	}{{syscall.SIGINT, 130}, {syscall.SIGTERM, 143}} {
		f := newWatchFixture(t, 50*time.Millisecond)
		start := time.Now()
		f.sig <- tc.sig
		f.waitDone(t)
		if code := f.waitExit(t, 5*time.Second); code != tc.want {
			t.Errorf("%v: exit(%d), want %d", tc.sig, code, tc.want)
		}
		if took := time.Since(start); took < 50*time.Millisecond {
			t.Errorf("%v: exit after %v, want the 50ms grace first", tc.sig, took)
		}
		f.stop()
		if got := f.errOut.String(); got != "\n" {
			t.Errorf("%v: errOut %q, want a newline", tc.sig, got)
		}
	}
}

// A command that returns within grace ends the watcher with no exit.
func TestWatchSignalsNoExitWhenWorkReturns(t *testing.T) {
	f := newWatchFixture(t, 100*time.Millisecond)
	f.sig <- syscall.SIGINT
	f.waitDone(t)
	f.stop()
	time.Sleep(200 * time.Millisecond)
	select {
	case code := <-f.exits:
		t.Fatalf("exit(%d) called after stop, want none", code)
	default:
	}
	if got := f.errOut.String(); got != "" {
		t.Errorf("errOut %q, want nothing", got)
	}
}

func TestWatchSignalsSecondSignalExitsAtOnce(t *testing.T) {
	f := newWatchFixture(t, time.Hour)
	f.sig <- syscall.SIGINT
	f.waitDone(t)
	f.sig <- syscall.SIGINT
	if code := f.waitExit(t, 5*time.Second); code != 130 {
		t.Errorf("exit(%d), want 130", code)
	}
	f.stop()
	if got := f.errOut.String(); got != "\n" {
		t.Errorf("errOut %q, want a newline", got)
	}
}

// stop ends the watcher, so a later signal does nothing.
func TestWatchSignalsStopEndsWatcher(t *testing.T) {
	f := newWatchFixture(t, time.Millisecond)
	f.stop()
	f.sig <- syscall.SIGINT
	select {
	case code := <-f.exits:
		t.Fatalf("exit(%d) called after stop, want none", code)
	default:
	}
	if len(f.sig) != 1 {
		t.Errorf("watcher read a signal after stop")
	}
}

// Ctrl-C at a self prompt exits even though the prompt blocks on stdin.
func TestMainSelfInterruptAtPrompt(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdinW.Close() }()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outR.Close() }()
	env := []string{runMainEnv + "=1", "PATH=", "XDG_CONFIG_HOME=" + t.TempDir(), "TILT_PORT=10399", "HOME=" + t.TempDir()}
	p, err := os.StartProcess(self, []string{self, "self", "channel", "nightly"},
		&os.ProcAttr{Env: env, Files: []*os.File{stdinR, outW, outW}})
	if err != nil {
		t.Fatal(err)
	}
	_ = stdinR.Close()
	_ = outW.Close()

	// The prompt prints after main registers for signals.
	prompted := make(chan string, 1)
	go func() {
		var out []byte
		buf := make([]byte, 512)
		for {
			n, err := outR.Read(buf)
			out = append(out, buf[:n]...)
			if bytes.Contains(out, []byte("[y/N]")) || err != nil {
				prompted <- string(out)
				return
			}
		}
	}()
	select {
	case out := <-prompted:
		if !strings.Contains(out, "[y/N]") {
			_ = p.Kill()
			_, _ = p.Wait()
			t.Fatalf("no prompt, output %q", out)
		}
	case <-time.After(5 * time.Second):
		_ = p.Kill()
		_, _ = p.Wait()
		t.Fatal("no prompt within 5s")
	}
	if err := p.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	st := waitExit(t, p, 3*time.Second)
	if st.ExitCode() != 130 {
		t.Fatalf("exit %d (%v), want 130", st.ExitCode(), st)
	}
}
