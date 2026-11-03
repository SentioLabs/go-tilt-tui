package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// buttonTimeout bounds one ClickButton call from the buttons overlay.
const buttonTimeout = 15 * time.Second

// buttonConflictText is the flash for an HTTP 409. The UI doesn't retry.
const buttonConflictText = "button changed, try again"

// buttonResultMsg reports one ClickButton call from the buttons overlay.
type buttonResultMsg struct {
	sent tilt.Button // the button as clicked
	got  tilt.Button // what ClickButton returned
	err  error
	at   time.Time
}

func init() {
	register(Feature{
		Name: "buttons",
		Commands: func(m *Model) []command.Command {
			return []command.Command{{
				ID: "buttons", Title: "resource buttons", Keys: []string{"b"}, Scope: command.ScopeGlobal,
				Enabled: func(ctx command.Context) bool { return ctx.Resource != nil },
				Run:     func(ctx command.Context) tea.Cmd { return m.openButtons(ctx.Resource.Name()) },
			}}
		},
		Update: updateButtons,
		Hints:  []Hint{{ID: "buttons", Label: "buttons"}},
	})
}

// openButtons pushes the button list for resource, or flashes when it
// has none.
func (m *Model) openButtons(resource string) tea.Cmd {
	bs := m.store.Buttons(resource)
	if len(bs) == 0 {
		return m.flashCmd(termsafe.Text(resource)+" has no buttons", 0)
	}
	return msgCmd(PushOverlayMsg{Overlay: overlay.NewButtons(m.d.Theme, resource, bs)})
}

func updateButtons(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case overlay.ClickMsg:
		return m.clickButton(msg.Button, msg.Values), true
	case buttonResultMsg:
		m.buttonResult(msg)
		return nil, true
	}
	return nil, false
}

// clickButton flashes the start and runs ClickButton in a command.
func (m *Model) clickButton(b tilt.Button, values map[string]any) tea.Cmd {
	client, now := m.d.Client, m.d.Now
	text := buttonLabel(b)
	return tea.Batch(
		msgCmd(flashMsg{text: "clicking " + text + "…", at: now()}),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), buttonTimeout)
			defer cancel()
			got, err := client.ClickButton(ctx, b, values)
			return buttonResultMsg{sent: b, got: got, err: err, at: now()}
		},
	)
}

// buttonResult flashes the outcome. On success it stores the returned
// button, unless a newer one already arrived over the websocket.
func (m *Model) buttonResult(r buttonResultMsg) {
	switch {
	case errors.Is(r.err, tilt.ErrConflict):
		m.setFlash(buttonConflictText, r.at, flashFor)
	case r.err != nil:
		m.setFlash("button failed: "+termsafe.Text(r.err.Error()), r.at, flashFor)
	default:
		keepClickedButton(m.store, r.sent, r.got)
		m.setFlash("clicked "+buttonLabel(r.sent), r.at, flashFor)
	}
}

// keepClickedButton stores got only while the store still holds sent at
// the resourceVersion that was clicked. A newer button from the
// websocket wins.
func keepClickedButton(s *tilt.Store, sent, got tilt.Button) {
	cur, ok := s.Button(sent.Metadata.Name)
	if !ok || cur.Metadata.ResourceVersion != sent.Metadata.ResourceVersion {
		return
	}
	s.Apply(tilt.View{UIButtons: []tilt.Button{got}})
}

// buttonLabel is the button's text, or its name when the text is empty.
func buttonLabel(b tilt.Button) string {
	if b.Spec.Text != "" {
		return termsafe.Text(b.Spec.Text)
	}
	return termsafe.Text(b.Metadata.Name)
}
