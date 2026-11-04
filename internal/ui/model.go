// Package ui is the Bubble Tea front end. It owns the tilt.Store and only
// mutates it from Update, so no locking is needed.
package ui

import (
	"context"
	"errors"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/logview"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/resourcelist"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

const (
	// flashFor is how long a footer message stays up.
	flashFor = 3 * time.Second
	// startupFlashFor is how long Deps.Flash stays up.
	startupFlashFor = 10 * time.Second
	// tickEvery refreshes relative times, the uptime and flash expiry.
	tickEvery = time.Second

	allResourcesTitle = "All Resources"
)

// Messages sent into the program from the watch goroutine.
type (
	// ViewMsg carries one /ws/view message.
	ViewMsg tilt.View
	// ConnMsg reports a connection state change. Err is nil while
	// (re)connecting and set after a drop.
	ConnMsg struct {
		Connecting bool
		Err        error
	}
)

// Private messages that commands and ticks send back to Update.
type (
	// flashMsg shows text in the footer for d, or for flashFor when d is
	// zero. at orders flashes that race each other, such as a trigger's
	// start and its result.
	flashMsg struct {
		text string
		at   time.Time
		d    time.Duration
	}
	switchPaneMsg struct{}
	showPaneMsg   struct{ focus command.Focus }
	tickMsg       time.Time
)

// Client is the slice of tilt.Client the UI needs. Tests fake it.
type Client interface {
	Trigger(ctx context.Context, name string) error
	// ClickButton records a click on b with the given input values and
	// returns the updated button. It returns tilt.ErrConflict on HTTP 409.
	ClickButton(ctx context.Context, b tilt.Button, values map[string]any) (tilt.Button, error)
}

// errNoOpener is what Deps.Open returns when main didn't set it.
var errNoOpener = errors.New("no URL opener configured")

type Deps struct {
	Client  Client
	Addr    string
	Theme   theme.Theme
	Logs    *logstore.Store
	Filters []string         // active saved-filter names, shown in the log title
	Remote  bool             // --allow-remote: show "⚠ unencrypted remote"
	Flash   string           // startup warnings
	Now     func() time.Time // nil means time.Now
	// Open opens an http or https URL in the browser. main passes
	// proc.OpenURL. nil means every call fails with errNoOpener.
	Open func(ctx context.Context, url string) error
	// Dependencies returns each resource's resourceDependencies from
	// Tilt's engine dump. main passes tilt.Client.ResourceDependencies.
	// nil means the info panel shows "dependency info unavailable".
	Dependencies func(ctx context.Context) (map[string][]string, error)
	// FileWatches returns Tilt's file watches. main passes a func that
	// runs `tilt get filewatches` against the local Tilt. nil means the
	// file-watch panel shows "file-watch info needs a local Tilt", which
	// is what --allow-remote gets.
	FileWatches func(ctx context.Context) ([]tilt.FileWatch, error)
	// WheelList is the mouseWheelList setting: "cursor" moves the list
	// cursor on a wheel notch over the list, "scroll" scrolls the list
	// view. "" means "cursor".
	WheelList string
}

type connState int

const (
	connConnecting connState = iota
	connLive
	connDown
)

type Model struct {
	d        Deps
	store    *tilt.Store
	list     *resourcelist.Model
	logs     *logview.Model
	overlays []overlay.Overlay

	focus     command.Focus
	width     int
	height    int
	conn      connState
	connErr   error
	startTime time.Time // from View.TiltStartTime
	snapshots int       // complete snapshots seen

	flash      string
	flashAt    time.Time // when the shown flash was issued
	flashUntil time.Time
	ticking    bool

	// fstate holds each feature's state by Feature.Name. Value copies of
	// Model share the map, which is what keeps state across Update.
	fstate map[string]any
}

func New(d Deps) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Open == nil {
		d.Open = func(context.Context, string) error { return errNoOpener }
	}
	m := Model{
		d:     d,
		store: tilt.NewStore(),
		list:  resourcelist.New(d.Theme),
		logs:  logview.New(d.Logs, d.Theme),

		fstate: map[string]any{},
	}
	if d.Flash != "" {
		m.setFlash(d.Flash, d.Now(), startupFlashFor)
	}
	m.syncSource()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// Update offers msg to every feature, then handles it, then points the log
// view at the list's selection and starts the tick if one is due.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	handled := false
	for _, f := range features {
		if f.Update == nil {
			continue
		}
		cmd, h := f.Update(&m, msg)
		cmds = append(cmds, cmd)
		if h {
			handled = true
			break
		}
	}
	if !handled {
		var cmd tea.Cmd
		m, cmd = m.handle(msg)
		cmds = append(cmds, cmd)
	}
	m.syncSource()
	if !m.ticking && m.needsTick() {
		m.ticking = true
		cmds = append(cmds, tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) }))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handle(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case ViewMsg:
		m.applyView(tilt.View(msg))

	case ConnMsg:
		if msg.Connecting {
			m.conn = connConnecting
		} else {
			m.conn, m.connErr = connDown, msg.Err
		}

	case tea.KeyPressMsg:
		return m.routeKey(msg)

	case tea.MouseWheelMsg:
		m.logs.HandleMouse(msg)

	case PushOverlayMsg:
		if msg.Overlay != nil {
			m.overlays = append(slices.Clip(m.overlays), msg.Overlay)
		}

	case overlay.CloseMsg:
		if n := len(m.overlays); n > 0 {
			m.overlays = m.overlays[:n-1]
		}

	case flashMsg:
		d := msg.d
		if d <= 0 {
			d = flashFor
		}
		m.setFlash(msg.text, msg.at, d)

	case switchPaneMsg:
		if m.focus == command.FocusList {
			m.focus = command.FocusLogs
		} else {
			m.focus = command.FocusList
		}

	case showPaneMsg:
		m.focus = msg.focus

	case tickMsg:
		m.ticking = false
		if m.flash != "" && !m.flashActive() {
			m.flash = ""
		}
	}
	return m, nil
}

func (m *Model) applyView(v tilt.View) {
	m.conn, m.connErr = connLive, nil
	m.store.Apply(v)
	m.d.Logs.Append(v)
	m.list.SetResources(m.store.Resources())
	if !v.TiltStartTime.IsZero() {
		m.startTime = v.TiltStartTime
	}
	if v.IsComplete {
		m.snapshots++
		if m.snapshots > 1 {
			m.setFlash("reconnected · logs reloaded", m.d.Now(), flashFor)
		}
	}
}

// setFlash shows text until at+d. A flash issued before the one on screen
// is dropped, so a slow message can't hide a newer one.
func (m *Model) setFlash(text string, at time.Time, d time.Duration) {
	if at.Before(m.flashAt) {
		return
	}
	m.flash, m.flashAt, m.flashUntil = text, at, at.Add(d)
}

func (m *Model) flashActive() bool {
	return m.flash != "" && m.d.Now().Before(m.flashUntil)
}

func (m *Model) showsUptime() bool {
	return m.conn == connLive && !m.startTime.IsZero()
}

// needsTick reports whether anything on screen changes with time alone.
// It renders first, so the list reports the relative times this update
// put on screen rather than those of the previous frame.
func (m *Model) needsTick() bool {
	if m.showsUptime() || m.flashActive() {
		return true
	}
	m.render()
	return m.list.ShowsRelativeTime()
}

// syncSource points the log view at the list's selection.
func (m *Model) syncSource() {
	sel := m.list.Selected()
	if sel == m.logs.Source() {
		return
	}
	title := allResourcesTitle
	if !sel.All {
		title = termsafe.Text(sel.Resource)
	}
	m.logs.SetSource(sel, title)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "tilt-tui " + termsafe.Text(m.d.Addr)
	return v
}
