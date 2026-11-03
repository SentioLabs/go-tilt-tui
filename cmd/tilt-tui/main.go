// Command tilt-tui is a terminal UI for a running Tilt instance.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/config"
	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

// checkHostTimeout bounds the startup loopback check.
const checkHostTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tilt-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	host := flag.String("host", tilt.DefaultHost, "Tilt API host")
	port := flag.Int("port", defaultPort(), "Tilt API port (default from $TILT_PORT, else 10350)")
	allowRemote := flag.Bool("allow-remote", false, "allow a non-loopback --host; Tilt's tokens then travel unencrypted")
	flag.Parse()

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	loaded, err := config.Load(dir)
	if err != nil {
		return err
	}
	th, themeWarning := theme.Select(loaded.Config.Theme)

	client, err := tilt.New(*host, *port, tilt.Options{AllowRemote: *allowRemote})
	if err != nil {
		return err
	}
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), checkHostTimeout)
	err = client.CheckHost(checkCtx)
	cancelCheck()
	if err != nil {
		return err
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := tea.NewProgram(ui.New(ui.Deps{
		Client:  client,
		Addr:    client.Addr(),
		Theme:   th,
		Logs:    logs,
		Filters: filterNames,
		Remote:  *allowRemote,
		Flash:   strings.Join(warnings, " · "),
	}))
	go watch(ctx, client, p)

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
// UI survives `tilt down && tilt up` without a restart.
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
