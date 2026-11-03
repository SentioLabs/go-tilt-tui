package proc

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
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

func equalCalls(a, b []call) bool {
	return slices.EqualFunc(a, b, func(x, y call) bool {
		return x.name == y.name && slices.Equal(x.args, y.args)
	})
}
