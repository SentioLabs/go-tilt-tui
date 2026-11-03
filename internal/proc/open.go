package proc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// goos picks the opener. Tests replace it.
var goos = runtime.GOOS

// openerWaitDelay bounds how long runOpener waits for the output pipe to
// close after the opener exits. xdg-open can start a browser that
// inherits the pipe and holds it open until the browser exits.
const openerWaitDelay = time.Second

// runOpener runs one opener command and waits for it to exit. Tests
// replace it, so no test ever starts a browser. The child gets no stdin
// and its output is captured, so a terminal browser can't take over the
// TUI's terminal.
var runOpener = func(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = openerWaitDelay
	out, err := cmd.CombinedOutput()
	// ErrWaitDelay means the opener exited 0 and a child it started still
	// holds the output pipe. The opener succeeded.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			i := 200
			for i > 0 && !utf8.RuneStart(msg[i]) {
				i--
			}
			msg = msg[:i]
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
	// url.Parse misses a control character in the fragment.
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return errors.New("open URL: refusing a URL with a control character")
	}
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
	// url.URL keeps RawQuery verbatim, so encode its unsafe bytes first.
	u.RawQuery = escapeQuery(u.RawQuery)
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

// escapeQuery percent-encodes every byte of q that RFC 3986 doesn't allow
// in a query. Allowed bytes and existing %XX escapes stay as they are, so
// the query keeps its meaning.
func escapeQuery(q string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		c := q[i]
		if queryByte(c) || c == '%' && i+2 < len(q) && isHex(q[i+1]) && isHex(q[i+2]) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// queryByte reports whether RFC 3986 allows c unencoded in a query. It
// leaves out '%', which escapeQuery handles.
func queryByte(c byte) bool {
	if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@/?", c) >= 0
}

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
