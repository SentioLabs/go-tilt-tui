package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

const (
	// togglePendingFor is the longest a toggle stays pending without
	// Tilt sending the button back with a changed action.
	togglePendingFor = 10 * time.Second
	// toggleDoneFor is how long the footer shows the result.
	toggleDoneFor = 5 * time.Second
	// toggleTimeout bounds one ClickButton call.
	toggleTimeout = 15 * time.Second
	// toggleActionInput is the disable toggle's hidden input. Tilt sets
	// it to "on" or "off" and flips it once it has handled a click.
	toggleActionInput = "action"
	// toggleConflictText is the flash for an HTTP 409. ctrl+d doesn't
	// retry.
	toggleConflictText = "button changed, try again"
)

// togglePending is one disable toggle waiting for Tilt to flip its
// action input.
type togglePending struct {
	seq     int
	button  string // the toggle's name
	action  string // the action value that was submitted
	disable bool   // true when the click disables the resource
	since   time.Time
}

// toggleState is the disable feature's state on one model. It lives in
// featureState, so every copy of the model shares it and no two models
// do.
type toggleState struct {
	seq        int
	pending    map[string]togglePending // by resource name
	confirming bool                     // a y/n overlay is open
}

func (m *Model) toggles() *toggleState {
	return featureState(m, "disable", func() *toggleState {
		return &toggleState{pending: map[string]togglePending{}}
	})
}

// Messages the disable feature sends itself.
type (
	// toggleAnswerMsg is the reply to "disable api? y/n".
	toggleAnswerMsg struct {
		resource string
		disable  bool
		yes      bool
	}
	// toggleResultMsg reports one ClickButton call.
	toggleResultMsg struct {
		seq      int
		resource string
		disable  bool
		sent     tilt.Button
		got      tilt.Button
		err      error
		at       time.Time
	}
	// toggleExpireMsg wakes the feature to drop toggles pending for
	// togglePendingFor.
	toggleExpireMsg struct{}
)

func init() {
	register(Feature{
		Name: "disable",
		Commands: func(m *Model) []command.Command {
			return []command.Command{{
				ID: "disable", Title: "disable/enable resource", Keys: []string{"ctrl+d"}, Scope: command.ScopeGlobal,
				Enabled: func(ctx command.Context) bool { return ctx.Resource != nil },
				Run:     func(ctx command.Context) tea.Cmd { return m.pressToggle(*ctx.Resource) },
			}}
		},
		Update:      updateToggle,
		StatusParts: togglePendingParts,
		Hints:       []Hint{{ID: "disable", Label: "disable"}},
	})
}

// pressToggle handles ctrl+d on r. It does nothing while r's toggle is
// pending or a y/n question is open. It asks first when the toggle
// requires confirmation.
func (m *Model) pressToggle(r tilt.Resource) tea.Cmd {
	s := m.toggles()
	name := r.Name()
	if _, busy := s.pending[name]; busy || s.confirming {
		return nil
	}
	b, ok := m.store.DisableButton(name)
	if !ok {
		return m.flashCmd(termsafe.Text(name)+" has no disable toggle", 0)
	}
	disable := !r.IsDisabled()
	if b.Spec.RequiresConfirmation {
		s.confirming = true
		return msgCmd(PushOverlayMsg{Overlay: &toggleConfirm{resource: name, disable: disable}})
	}
	return m.clickToggle(name, disable)
}

// clickToggle marks resource's toggle pending and clicks it with its
// current values. It reads the toggle from the store at click time.
func (m *Model) clickToggle(resource string, disable bool) tea.Cmd {
	s := m.toggles()
	if _, busy := s.pending[resource]; busy {
		return nil
	}
	b, ok := m.store.DisableButton(resource)
	if !ok {
		return m.flashCmd(termsafe.Text(resource)+" has no disable toggle", 0)
	}
	values := map[string]any{}
	for _, in := range b.Spec.Inputs {
		if in.Hidden != nil {
			values[in.Name] = in.Hidden.Value
		}
	}
	s.seq++
	p := togglePending{
		seq:     s.seq,
		button:  b.Metadata.Name,
		action:  toggleAction(b),
		disable: disable,
		since:   m.d.Now(),
	}
	s.pending[resource] = p
	client, now := m.d.Client, m.d.Now
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), toggleTimeout)
			defer cancel()
			got, err := client.ClickButton(ctx, b, values)
			return toggleResultMsg{seq: p.seq, resource: resource, disable: disable, sent: b, got: got, err: err, at: now()}
		},
		tea.Tick(togglePendingFor, func(time.Time) tea.Msg { return toggleExpireMsg{} }),
	)
}

func updateToggle(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	s := m.toggles()
	switch msg := msg.(type) {
	case toggleAnswerMsg:
		s.confirming = false
		if !msg.yes {
			return nil, true
		}
		return m.clickToggle(msg.resource, msg.disable), true

	case toggleResultMsg:
		m.toggleResult(msg)
		return nil, true

	case ViewMsg:
		// A pending toggle clears when Tilt sends its button back with a
		// different action. A resource's disable state can change first:
		// the root model draws it, and the toggle stays pending.
		for _, b := range msg.UIButtons {
			for res, p := range s.pending {
				if p.button == b.Metadata.Name && b.Metadata.DeletionTimestamp == nil && toggleAction(b) != p.action {
					delete(s.pending, res)
				}
			}
		}

	case toggleExpireMsg, tickMsg:
		now := m.d.Now()
		for res, p := range s.pending {
			if now.Sub(p.since) >= togglePendingFor {
				delete(s.pending, res)
			}
		}
		_, own := msg.(toggleExpireMsg)
		return nil, own
	}
	return nil, false
}

// toggleResult flashes the outcome of a click. A failed click clears its
// pending toggle. A successful one stores the returned button, unless a
// newer one already arrived over the websocket.
func (m *Model) toggleResult(r toggleResultMsg) {
	s := m.toggles()
	name := termsafe.Text(r.resource)
	if r.err != nil {
		if p, ok := s.pending[r.resource]; ok && p.seq == r.seq {
			delete(s.pending, r.resource)
		}
		text := "toggle failed: " + termsafe.Text(r.err.Error())
		if errors.Is(r.err, tilt.ErrConflict) {
			text = toggleConflictText
		}
		m.setFlash(text, r.at, flashFor)
		return
	}
	if cur, ok := m.store.Button(r.sent.Metadata.Name); ok && cur.Metadata.ResourceVersion == r.sent.Metadata.ResourceVersion {
		m.store.Apply(tilt.View{UIButtons: []tilt.Button{r.got}})
	}
	text := "enabled " + name + " · ctrl+d again to disable"
	if r.disable {
		text = "disabled " + name + " · ctrl+d again to re-enable"
	}
	m.setFlash(text, r.at, toggleDoneFor)
}

// togglePendingParts shows each pending toggle in the status line, in
// resource order.
func togglePendingParts(m *Model) []string {
	s := m.toggles()
	if len(s.pending) == 0 {
		return nil
	}
	var parts []string
	for _, r := range m.store.Resources() {
		p, ok := s.pending[r.Name()]
		if !ok {
			continue
		}
		verb := "enabling "
		if p.disable {
			verb = "disabling "
		}
		parts = append(parts, m.d.Theme.Status[tilt.StatusPending].Render(verb+termsafe.Text(r.Name())+"…"))
	}
	return parts
}

// toggleAction returns the value of b's hidden action input.
func toggleAction(b tilt.Button) string {
	for _, in := range b.Spec.Inputs {
		if in.Name == toggleActionInput && in.Hidden != nil {
			return in.Hidden.Value
		}
	}
	return ""
}

// toggleConfirm asks "disable api? y/n". y clicks the toggle. Any of n,
// esc or q closes it without a click.
type toggleConfirm struct {
	resource string
	disable  bool
}

func (c *toggleConfirm) Title() string { return "Confirm" }

func (c *toggleConfirm) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	answer := func(yes bool) tea.Cmd {
		return tea.Batch(
			msgCmd(overlay.CloseMsg{}),
			msgCmd(toggleAnswerMsg{resource: c.resource, disable: c.disable, yes: yes}),
		)
	}
	switch k.String() {
	case "y", "Y":
		return c, answer(true)
	case "n", "N", "esc", "q":
		return c, answer(false)
	}
	return c, nil
}

func (c *toggleConfirm) View(width, height int) string {
	verb := "enable "
	if c.disable {
		verb = "disable "
	}
	return ansi.Truncate(verb+termsafe.Text(c.resource)+"? y/n", width, "…")
}
