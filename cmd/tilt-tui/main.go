// Command tilt-tui is a terminal UI for a running Tilt instance.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/config"
	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/proc"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
	"github.com/sentiolabs/go-tilt-tui/internal/update"
	"github.com/sentiolabs/go-tilt-tui/internal/version"
)

// checkHostTimeout bounds the startup loopback check.
const checkHostTimeout = 5 * time.Second

// errSignaled is what run returns when a signal ends --up before the TUI
// starts. main exits 1 without printing it.
var errSignaled = errors.New("stopped by a signal")

func main() {
	err := run()
	if errors.Is(err, errSignaled) {
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tilt-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	// parseFlags rejects positional arguments, so `self` goes first.
	if len(os.Args) > 1 && os.Args[1] == "self" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		return runSelf(ctx, update.New(dir, version.Version, os.Stdout, os.Stderr, os.Stdin), os.Args[2:], os.Stdout)
	}

	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if opts.version {
		fmt.Println(version.String())
		return nil
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	loaded, err := config.Load(dir)
	if err != nil {
		return err
	}
	th, themeWarning := theme.Select(loaded.Config.Theme)

	client, err := tilt.New(opts.host, opts.port, tilt.Options{AllowRemote: opts.allowRemote})
	if err != nil {
		return err
	}
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), checkHostTimeout)
	err = client.CheckHost(checkCtx)
	cancelCheck()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// findTilt resolves the tilt binary once, on first use. --up needs
	// it at startup; the file-watch panel needs it only when opened.
	findTilt := sync.OnceValues(func() (proc.Tilt, error) {
		return proc.FindTilt(loaded.Config.TiltBinaryPath)
	})

	var sup *supervisor
	if opts.up {
		if err := checkPortFree(opts.host, opts.port); err != nil {
			return err
		}
		t, err := findTilt()
		if err != nil {
			return err
		}
		// tilt runs in its own process group, so the terminal's signals
		// never reach it. sigCtx catches them instead: during the startup
		// window Up stops tilt when it ends, and later it quits the TUI so
		// the deferred Close stops tilt.
		sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		sup = &supervisor{ctx: sigCtx, start: tiltStarter(t), port: opts.port, args: opts.tiltArgs}
	}

	logs := logstore.New(logstore.Options{
		MaxBytesPerResource: loaded.Config.LogMaxBytesPerResource,
		Hide:                hide(loaded.Filters),
	})

	warnings := loaded.Warnings
	if themeWarning != "" {
		warnings = append(warnings, themeWarning)
	}
	filterNames := make([]string, 0, len(loaded.Filters))
	for _, f := range loaded.Filters {
		filterNames = append(filterNames, f.Name)
	}

	p := tea.NewProgram(ui.New(ui.Deps{
		Client:       client,
		Addr:         client.Addr(),
		Theme:        th,
		Logs:         logs,
		Filters:      filterNames,
		Remote:       opts.allowRemote,
		Flash:        strings.Join(warnings, " · "),
		Open:         proc.OpenURL,
		Dependencies: client.ResourceDependencies,
		FileWatches:  fileWatches(opts.allowRemote, opts.port, findTilt),
		WheelList:    loaded.Config.MouseWheelList,
	}), programOptions(sup)...)
	loop := &watchLoop{run: func(ctx context.Context) { watch(ctx, client, p) }}

	if sup == nil {
		loop.Start(ctx)
	} else {
		sup.send, sup.watch = p.Send, loop
		// launch waits out tilt's startup window before the TUI takes
		// the screen, so an early failure prints as a plain error.
		// Its text holds tilt's stderr, so it is made safe first.
		if err := sup.launch(); err != nil {
			if errors.Is(err, context.Canceled) {
				return errSignaled
			}
			return safeStartError(err)
		}
		// Deferred after cancel and stop, so it runs first: the child
		// gets SIGTERM and its grace period before anything else ends.
		defer func() {
			if err := sup.Close(); err != nil {
				fmt.Fprintln(os.Stderr, "tilt-tui: stopping tilt:", err)
			}
		}()
		// A signal quits the TUI the way q does. Bubble Tea doesn't catch
		// SIGHUP, and a closed terminal sends it. stop ends sigCtx on
		// return, and Quit after Run is a no-op, so this never blocks.
		go func() {
			<-sup.ctx.Done()
			p.Quit()
		}()
	}

	_, err = p.Run()
	return err
}

// hide returns the log store's Hide func for the saved filters: a line is
// hidden when any pattern of any filter matches it. It returns nil for no
// filters.
func hide(filters []config.Filter) func(string) bool {
	if len(filters) == 0 {
		return nil
	}
	return func(plain string) bool {
		for _, f := range filters {
			for _, p := range f.Patterns {
				if p.MatchString(plain) {
					return true
				}
			}
		}
		return false
	}
}

// watch keeps a /ws/view connection open, reconnecting after drops so the
// UI survives `tilt down && tilt up` without a restart. It returns when
// ctx ends. Under --up the supervisor cancels it when the child exits.
func watch(ctx context.Context, c *tilt.Client, p *tea.Program) {
	const retryEvery = 2 * time.Second
	for {
		p.Send(ui.ConnMsg{Connecting: true})
		err := c.Watch(ctx, func(v tilt.View) { p.Send(ui.ViewMsg(v)) })
		if ctx.Err() != nil {
			return
		}
		p.Send(ui.ConnMsg{Err: err})
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryEvery):
		}
	}
}

func defaultPort() int {
	if p, err := strconv.Atoi(os.Getenv("TILT_PORT")); err == nil {
		return p
	}
	return tilt.DefaultPort
}
