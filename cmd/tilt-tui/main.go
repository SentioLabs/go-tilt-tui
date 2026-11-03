// Command tilt-tui is a terminal UI for a running Tilt instance.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tilt-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	host := flag.String("host", tilt.DefaultHost, "Tilt API host")
	port := flag.Int("port", defaultPort(), "Tilt API port (default from $TILT_PORT, else 10350)")
	flag.Parse()

	client, err := tilt.New(*host, *port, tilt.Options{})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := tea.NewProgram(ui.New(client, client.Addr()))
	go watch(ctx, client, p)

	_, err = p.Run()
	return err
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
