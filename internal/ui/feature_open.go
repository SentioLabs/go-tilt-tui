package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// openTimeout bounds one Deps.Open call.
const openTimeout = 10 * time.Second

// openFeature opens the selected resource's endpoint link. o opens a lone
// link at once and shows a menu for several. The menu overlay holds its
// own state, so this feature keeps none.
var openFeature = Feature{
	Name:     "open",
	Commands: openCommands,
	Hints:    []Hint{{ID: "open-link", Label: "open"}},
}

func init() { register(openFeature) }

func openCommands(m *Model) []command.Command {
	return []command.Command{
		{ID: "open-link", Title: "open endpoint link", Keys: []string{"o"}, Scope: command.ScopeGlobal,
			Enabled: func(ctx command.Context) bool {
				return ctx.Resource != nil && len(ctx.Resource.Status.EndpointLinks) > 0
			},
			Run: func(ctx command.Context) tea.Cmd {
				links := ctx.Resource.Status.EndpointLinks
				open := func(url string) tea.Cmd { return openURL(m.d.Open, m.d.Now, url) }
				if len(links) == 1 {
					return open(links[0].URL)
				}
				return msgCmd(PushOverlayMsg{Overlay: overlay.NewLinks(m.d.Theme, ctx.Resource.Name(), links, open)})
			}},
	}
}

// openURL flashes the start, runs Deps.Open off the UI goroutine, then
// flashes the result. Deps.Open gets the raw URL and refuses any scheme
// other than http and https. The flash shows it through termsafe.Text.
func openURL(open func(context.Context, string) error, now func() time.Time, url string) tea.Cmd {
	shown := termsafe.Text(url)
	return tea.Batch(
		msgCmd(flashMsg{text: "opening " + shown + "…", at: now()}),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
			defer cancel()
			text := "opened " + shown
			if err := open(ctx, url); err != nil {
				text = "open failed: " + termsafe.Text(err.Error())
			}
			return flashMsg{text: text, at: now()}
		},
	)
}
