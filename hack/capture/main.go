// Command capture records /ws/view messages from a local Tilt into a JSONL
// fixture for tests. It replaces the working directory and $HOME in the
// output with /work and /home/user, so no developer paths get committed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

func main() {
	port := flag.Int("port", 10399, "Tilt API port (never 10350)")
	dur := flag.Duration("for", 40*time.Second, "how long to record")
	out := flag.String("out", "internal/tilt/testdata/views.jsonl", "output file")
	flag.Parse()
	if err := run(*port, *dur, *out); err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
}

func run(port int, dur time.Duration, out string) (err error) {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	c, err := tilt.New("localhost", port, tilt.Options{})
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	// Close can report a failed write on some filesystems, so it fails the run.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	n := 0
	var writeErr error
	for ctx.Err() == nil {
		err := c.Watch(ctx, func(v tilt.View) {
			b, err := json.Marshal(v)
			if err != nil {
				return
			}
			b = bytes.ReplaceAll(b, []byte(wd), []byte("/work"))
			if home != "" {
				b = bytes.ReplaceAll(b, []byte(home), []byte("/home/user"))
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				// A dropped line corrupts the fixture. Stop recording.
				writeErr = err
				cancel()
				return
			}
			n++
		})
		if writeErr != nil {
			return fmt.Errorf("write %s: %w", out, writeErr)
		}
		if err != nil && n == 0 {
			// Tilt isn't up yet. Retry until the deadline.
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			return fmt.Errorf("after %d messages: %w", n, err)
		}
	}
	fmt.Printf("recorded %d messages to %s\n", n, out)
	return nil
}
