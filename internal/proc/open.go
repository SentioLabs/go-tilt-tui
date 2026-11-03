package proc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// goos picks the opener. Tests replace it.
var goos = runtime.GOOS

// runOpener runs one opener command and waits for it to exit. Tests
// replace it, so no test ever starts a browser. The child gets no stdin
// and its output is captured, so a terminal browser can't take over the
// TUI's terminal.
var runOpener = func(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg != "" {
			return fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// openers returns the commands to try in order on system.
func openers(system string) []string {
	switch system {
	case "darwin":
		return []string{"open"}
	case "linux":
		// wslview is the WSL opener. Plain Linux rarely has it.
		return []string{"xdg-open", "wslview"}
	}
	return nil
}

// OpenURL opens raw in the browser. It refuses any scheme other than http
// and https.
func OpenURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("open URL: %w", err)
	}
	// url.Parse lowercases the scheme, so HTTP:// passes too.
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("open URL: refusing scheme %q: only http and https open", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("open URL: refusing a URL with no host")
	}
	cmds := openers(goos)
	if len(cmds) == 0 {
		return fmt.Errorf("open URL: no opener for %s", goos)
	}
	// The URL is re-encoded by url.URL and passed as one argument. No
	// shell sees it.
	target := u.String()
	var errs []error
	for _, name := range cmds {
		err := runOpener(ctx, name, target)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	return fmt.Errorf("open URL: %w", errors.Join(errs...))
}
