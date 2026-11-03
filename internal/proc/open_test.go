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
	"unicode/utf8"
)

// call is one opener command a test recorded.
type call struct {
	name string
	args []string
}

// stubOpener replaces runOpener and goos until the test ends. fail lists
// the commands that return an error. The real runner never runs.
func stubOpener(t *testing.T, system string, fail ...string) *[]call {
	t.Helper()
	oldRun, oldOS := runOpener, goos
	t.Cleanup(func() { runOpener, goos = oldRun, oldOS })
	goos = system
	var calls []call
	runOpener = func(_ context.Context, name string, args ...string) error {
		calls = append(calls, call{name: name, args: args})
		if slices.Contains(fail, name) {
			return errors.New(name + " failed")
		}
		return nil
	}
	return &calls
}

func TestOpenURLRefusesBadURLs(t *testing.T) {
	bad := []string{
		"",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.com/",
		"mailto:a@example.com",
		"data:text/html,<script>",
		"//example.com/no-scheme",
		"example.com",
		"http://",
		"https:///path-only",
		"http://:8080/",
		"http://exa mple.com/",
		"http://example.com/\x1b]52;c;cGF5bG9hZA==\x07",
		"HTTP://host#frag\x1b",
		"http://host/?q=\u009b",
		"http://host/\x7f",
	}
	for _, raw := range bad {
		calls := stubOpener(t, "linux")
		if err := OpenURL(context.Background(), raw); err == nil {
			t.Errorf("OpenURL(%q) = nil, want an error", raw)
		}
		if len(*calls) != 0 {
			t.Errorf("OpenURL(%q) ran %v", raw, *calls)
		}
	}
}

func TestOpenURLMacOS(t *testing.T) {
	calls := stubOpener(t, "darwin")
	if err := OpenURL(context.Background(), "https://example.com/a?b=c"); err != nil {
		t.Fatal(err)
	}
	want := []call{{name: "open", args: []string{"https://example.com/a?b=c"}}}
	if !equalCalls(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestOpenURLLinux(t *testing.T) {
	calls := stubOpener(t, "linux")
	if err := OpenURL(context.Background(), "HTTP://LOCALHOST:8080/"); err != nil {
		t.Fatal(err)
	}
	want := []call{{name: "xdg-open", args: []string{"http://LOCALHOST:8080/"}}}
	if !equalCalls(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestOpenURLFallsBackToWslview(t *testing.T) {
	calls := stubOpener(t, "linux", "xdg-open")
	if err := OpenURL(context.Background(), "http://localhost:8080/"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{name: "xdg-open", args: []string{"http://localhost:8080/"}},
		{name: "wslview", args: []string{"http://localhost:8080/"}},
	}
	if !equalCalls(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestOpenURLBothOpenersFail(t *testing.T) {
	stubOpener(t, "linux", "xdg-open", "wslview")
	err := OpenURL(context.Background(), "http://localhost:8080/")
	if err == nil || !strings.Contains(err.Error(), "xdg-open failed") || !strings.Contains(err.Error(), "wslview failed") {
		t.Fatalf("OpenURL = %v, want both failures", err)
	}
}

func TestOpenURLUnsupportedOS(t *testing.T) {
	calls := stubOpener(t, "plan9")
	if err := OpenURL(context.Background(), "http://localhost/"); err == nil {
		t.Fatal("OpenURL on plan9 = nil, want an error")
	}
	if len(*calls) != 0 {
		t.Errorf("ran %v", *calls)
	}
}

// Unsafe query bytes arrive percent-encoded. Allowed bytes, including
// the separators that give the query its meaning, pass through.
func TestOpenURLEncodesQuery(t *testing.T) {
	tests := []struct{ raw, want string }{
		{"http://h/?q=a b", "http://h/?q=a%20b"},
		{`http://h/?q="x"`, "http://h/?q=%22x%22"},
		{"http://h/?q=`id`", "http://h/?q=%60id%60"},
		{"http://h/?q=<b>", "http://h/?q=%3Cb%3E"},
		{"http://h/?q=a|b", "http://h/?q=a%7Cb"},
		{`http://h/?q=a\b`, "http://h/?q=a%5Cb"},
		{"http://h/?q=a^b", "http://h/?q=a%5Eb"},
		{"http://h/?q={x}", "http://h/?q=%7Bx%7D"},
		{"http://h/?q=caf\u00e9", "http://h/?q=caf%C3%A9"},
		{"http://h/?q=100%", "http://h/?q=100%25"},
		{"http://h/?q=%zz", "http://h/?q=%25zz"},
		{"http://h/?a=1&b=2;c=3", "http://h/?a=1&b=2;c=3"},
		{"http://h/?q=%20", "http://h/?q=%20"},
		{"http://h/?q=%2f%2F", "http://h/?q=%2f%2F"},
		{"http://h/?r=/p?x:y@z!$'()*+,-._~", "http://h/?r=/p?x:y@z!$'()*+,-._~"},
	}
	for _, tt := range tests {
		calls := stubOpener(t, "darwin")
		if err := OpenURL(context.Background(), tt.raw); err != nil {
			t.Errorf("OpenURL(%q) = %v", tt.raw, err)
			continue
		}
		want := []call{{name: "open", args: []string{tt.want}}}
		if !equalCalls(*calls, want) {
			t.Errorf("OpenURL(%q) calls = %v, want %v", tt.raw, *calls, want)
		}
	}
}

// A failed opener that leaves ctx done stops the fallback.
func TestOpenURLStopsWhenCtxDone(t *testing.T) {
	stubOpener(t, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var names []string
	runOpener = func(_ context.Context, name string, _ ...string) error {
		names = append(names, name)
		cancel()
		return errors.New(name + " failed")
	}
	if err := OpenURL(ctx, "http://localhost:8080/"); err == nil {
		t.Fatal("OpenURL = nil, want an error")
	}
	if want := []string{"xdg-open"}; !slices.Equal(names, want) {
		t.Errorf("ran %v, want %v", names, want)
	}
}

// Shell metacharacters stay inside the one URL argument.
func TestOpenURLOneArgument(t *testing.T) {
	calls := stubOpener(t, "darwin")
	raw := "http://localhost:8080/a;rm -rf ~&b=$(id)|x"
	if err := OpenURL(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || len((*calls)[0].args) != 1 {
		t.Fatalf("calls = %v, want one call with one argument", *calls)
	}
	if arg := (*calls)[0].args[0]; !strings.HasPrefix(arg, "http://localhost:8080/") || strings.Contains(arg, " ") {
		t.Errorf("argument = %q, want the re-encoded URL", arg)
	}
}

// The real runner, exercised with true and false instead of a browser.
func TestRunOpenerReal(t *testing.T) {
	if err := runOpener(context.Background(), "true", "http://localhost/"); err != nil {
		t.Errorf("true: %v", err)
	}
	if err := runOpener(context.Background(), "false", "http://localhost/"); err == nil {
		t.Error("false: want an error")
	}
	if err := runOpener(context.Background(), "tilt-tui-no-such-opener"); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("missing opener = %v, want exec.ErrNotFound", err)
	}
}

// An opener that exits 0 but leaves a background child holding its
// output returns within the wait delay. xdg-open does this when it hands
// off to a browser.
func TestRunOpenerReturnsWhenChildHoldsOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := `sleep 5 & echo $! > "$0"; exit 0`
	start := time.Now()
	err := runOpener(context.Background(), "sh", "-c", script, pidFile)
	elapsed := time.Since(start)
	if b, readErr := os.ReadFile(pidFile); readErr == nil {
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(b))); convErr == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	} else {
		t.Errorf("read sleep PID: %v", readErr)
	}
	if err != nil {
		t.Errorf("runOpener = %v, want nil", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("runOpener took %s, want under 2s", elapsed)
	}
}

// Trimmed output never ends in half a rune.
func TestRunOpenerTrimsOutputOnRune(t *testing.T) {
	out := strings.Repeat("a", 199) + "\u00e9"
	err := runOpener(context.Background(), "sh", "-c", `printf '%s' "$0"; exit 1`, out)
	if err == nil {
		t.Fatal("runOpener = nil, want an error")
	}
	if !utf8.ValidString(err.Error()) {
		t.Errorf("error %q is not valid UTF-8", err)
	}
	if !strings.HasSuffix(err.Error(), ": "+strings.Repeat("a", 199)) {
		t.Errorf("error = %q, want the output trimmed to 199 bytes", err)
	}
}

func equalCalls(a, b []call) bool {
	return slices.EqualFunc(a, b, func(x, y call) bool {
		return x.name == y.name && slices.Equal(x.args, y.args)
	})
}
