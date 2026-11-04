package ui

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/command"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/theme"
)

var update = flag.Bool("update", false, "rewrite golden files")

var now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// --- Contract assertions ---

var (
	_ Client                                             = (*tilt.Client)(nil)
	_ Client                                             = (*fakeClient)(nil)
	_ func(context.Context, string) error                = Deps{}.Open
	_ func(context.Context) (map[string][]string, error) = Deps{}.Dependencies
	_ func(context.Context) ([]tilt.FileWatch, error)    = Deps{}.FileWatches
	_ func(*Model, string, time.Duration) tea.Cmd        = (*Model).flashCmd
	_ func(*Model) (string, bool)                        = Feature{}.ConnStatus
	_ func(*Model) []string                              = Feature{}.StatusParts
	_ func(*Model) string                                = Feature{}.ListTitle
	_ func(*Model) string                                = Feature{}.LogTitle
	_ []Hint                                             = Feature{}.Hints
	_ func(*Model) (string, string, bool)                = Feature{}.Panel
	_ command.ID                                         = Hint{}.ID
)

// click is one ClickButton call the fake recorded.
type click struct {
	button tilt.Button
	values map[string]any
}

type fakeClient struct {
	triggered []string
	err       error

	clicks   []click
	clickErr error
	// clickResult builds the button ClickButton returns. nil returns the
	// clicked button unchanged.
	clickResult func(tilt.Button) tilt.Button
}

func (f *fakeClient) Trigger(_ context.Context, name string) error {
	f.triggered = append(f.triggered, name)
	return f.err
}

func (f *fakeClient) ClickButton(_ context.Context, b tilt.Button, values map[string]any) (tilt.Button, error) {
	f.clicks = append(f.clicks, click{button: b, values: values})
	if f.clickErr != nil {
		return tilt.Button{}, f.clickErr
	}
	if f.clickResult != nil {
		return f.clickResult(b), nil
	}
	return b, nil
}

// withFeatures replaces the feature registry with fs until the test ends.
func withFeatures(t *testing.T, fs ...Feature) {
	t.Helper()
	saved := features
	t.Cleanup(func() { features = saved })
	features = fs
}

func newModel(t *testing.T, d Deps) (Model, *fakeClient) {
	t.Helper()
	fc := &fakeClient{}
	th, ok := theme.ByName("mono")
	if !ok {
		t.Fatal("mono theme missing")
	}
	d.Client, d.Addr, d.Theme = fc, "localhost:10350", th
	d.Logs = logstore.New(logstore.Options{})
	d.Now = func() time.Time { return now }
	return New(d), fc
}

// drain runs cmd and returns the messages produced within 100ms. Ticks
// that sleep longer are skipped. Batches are expanded.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, drain(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// send delivers msg, then feeds back every message its command produces
// (one level deep), except quit.
func send(m Model, msg tea.Msg) (Model, []tea.Msg) {
	nm, cmd := m.Update(msg)
	m = nm.(Model)
	out := drain(cmd)
	for _, r := range out {
		if _, quit := r.(tea.QuitMsg); quit {
			continue
		}
		nm, _ = m.Update(r)
		m = nm.(Model)
	}
	return m, out
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = send(m, key(k))
	}
	return m
}

func key(s string) tea.KeyPressMsg {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab}
	if c, ok := codes[s]; ok {
		return tea.KeyPressMsg{Code: c}
	}
	if s == "ctrl+c" {
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func snapshot(t *testing.T, complete bool) ViewMsg {
	t.Helper()
	fin := now.Add(-11 * time.Minute)
	start := fin.Add(-11400 * time.Millisecond)
	raw, err := json.Marshal(map[string]any{
		"isComplete":    complete,
		"tiltStartTime": now.Add(-(13*time.Minute + 21*time.Second)),
		"uiResources": []map[string]any{
			{"metadata": map[string]any{"name": "(Tiltfile)"}, "status": map[string]any{"order": 1, "updateStatus": "ok"}},
			{"metadata": map[string]any{"name": "api", "labels": map[string]string{"app": "backend"}},
				"status": map[string]any{"order": 2, "updateStatus": "error",
					"buildHistory": []map[string]any{{"startTime": start, "finishTime": fin, "error": "boom\x1b]0;pwned\x07"}}}},
			{"metadata": map[string]any{"name": "web\x1b]52;c;ZXZpbA==\x07", "labels": map[string]string{"app": "frontend"}},
				"status": map[string]any{"order": 3, "updateStatus": "ok"}},
		},
		"logList": map[string]any{
			"spans": map[string]any{
				"a": map[string]string{"manifestName": "api"},
				"w": map[string]string{"manifestName": "web\x1b]52;c;ZXZpbA==\x07"},
			},
			"segments": []map[string]any{
				{"spanId": "a", "time": now, "text": "api starting\n"},
				{"spanId": "w", "time": now, "text": "web ready\x1b]52;c;ZXZpbA==\x07\n"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var v tilt.View
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return ViewMsg(v)
}

func setup(t *testing.T, w, h int, d Deps) (Model, *fakeClient) {
	m, fc := newModel(t, d)
	m, _ = send(m, tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = send(m, ConnMsg{Connecting: true})
	m, _ = send(m, snapshot(t, true))
	return m, fc
}

func screen(m Model) string { return ansi.Strip(m.render()) }

func TestWideLayoutShowsBothPanes(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	s := screen(m)
	for _, want := range []string{"Resources", "✗ 1", "✓ 2/3", "All Resources", "api starting", "web ready", "● live", "localhost:10350", "13m 21s", "[follow]"} {
		if !strings.Contains(s, want) {
			t.Errorf("wide screen lacks %q:\n%s", want, s)
		}
	}
}

func TestNarrowLayoutOnePane(t *testing.T) {
	m, _ := setup(t, 80, 20, Deps{})
	if s := screen(m); strings.Contains(s, "api starting") {
		t.Fatalf("narrow list view also shows logs:\n%s", s)
	}
	m = press(m, "enter")
	s := screen(m)
	if !strings.Contains(s, "api starting") || strings.Contains(s, "▼ backend") {
		t.Fatalf("enter did not switch to the log pane alone:\n%s", s)
	}
	m = press(m, "esc")
	if s := screen(m); !strings.Contains(s, "▼ backend") {
		t.Fatalf("esc did not return to the list:\n%s", s)
	}
	m = press(m, "j", "enter") // enter on the backend header toggles it
	if s := screen(m); !strings.Contains(s, "▶ backend") || strings.Contains(s, "api starting") {
		t.Fatalf("enter on a header did not toggle the group:\n%s", s)
	}
}

func TestSelectionDrivesLogs(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	m = press(m, "j", "j") // ▼ backend, api
	s := screen(m)
	if !strings.Contains(s, "api starting") || strings.Contains(s, "web ready") {
		t.Fatalf("log pane not filtered to api:\n%s", s)
	}
}

func TestTrigger(t *testing.T) {
	m, fc := setup(t, 140, 20, Deps{})
	m = press(m, "r")
	if len(fc.triggered) != 0 {
		t.Fatal("r triggered something with All Resources selected")
	}
	m = press(m, "j", "j", "r")
	if !slices.Equal(fc.triggered, []string{"api"}) {
		t.Fatalf("triggered = %q", fc.triggered)
	}
	if s := screen(m); !strings.Contains(s, "triggered api") {
		t.Fatalf("no trigger flash:\n%s", s)
	}
}

func TestOverlayStack(t *testing.T) {
	m, _ := setup(t, 140, 30, Deps{})
	m = press(m, "?")
	if len(m.overlays) != 1 || !strings.Contains(screen(m), "Help") {
		t.Fatalf("help did not open:\n%s", screen(m))
	}
	before := m.list.Selected()
	m = press(m, "j")
	if m.list.Selected() != before {
		t.Fatal("a key reached the list while an overlay was open")
	}
	_, msgs := send(m, key("ctrl+c"))
	if !slices.ContainsFunc(msgs, func(x tea.Msg) bool { _, ok := x.(tea.QuitMsg); return ok }) {
		t.Fatal("ctrl+c did not quit with an overlay open")
	}
	m = press(m, "esc")
	if len(m.overlays) != 0 {
		t.Fatal("esc did not close the overlay")
	}
}

func TestReconnectFlash(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	if strings.Contains(screen(m), "reconnected") {
		t.Fatal("first snapshot flashed reconnect")
	}
	m, _ = send(m, snapshot(t, true))
	if !strings.Contains(screen(m), "reconnected · logs reloaded") {
		t.Fatalf("no reconnect flash:\n%s", screen(m))
	}
}

func TestStartupFlashAndRemoteMarker(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{Remote: true, Flash: `unknown theme "x", using default`, Filters: []string{"probes"}})
	s := screen(m)
	for _, want := range []string{"⚠ unencrypted remote", `unknown theme "x"`, "filters: probes"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func TestOnlySGRReachesTheTerminal(t *testing.T) {
	for _, w := range []int{80, 140} {
		m, _ := setup(t, w, 20, Deps{})
		for _, keys := range [][]string{nil, {"j", "j"}, {"?"}} {
			mm := press(m, keys...)
			rest := sgr.ReplaceAllString(mm.render(), "")
			if strings.ContainsAny(rest, "\x1b\x07\u009b") {
				t.Fatalf("width %d keys %v: non-SGR control in output: %q", w, keys, rest)
			}
		}
	}
}

type testOverlay struct{}

func (testOverlay) Update(tea.Msg) (overlay.Overlay, tea.Cmd) { return testOverlay{}, nil }
func (testOverlay) View(w, h int) string                      { return "test body" }
func (testOverlay) Title() string                             { return "Test" }

func TestFeatureRegistry(t *testing.T) {
	saved := features
	t.Cleanup(func() { features = saved })
	var seen int
	register(Feature{
		Name: "test",
		Commands: func(*Model) []command.Command {
			return []command.Command{{ID: "x", Title: "test overlay", Keys: []string{"x"}, Scope: command.ScopeGlobal,
				Run: func(command.Context) tea.Cmd {
					return func() tea.Msg { return PushOverlayMsg{Overlay: testOverlay{}} }
				}}}
		},
		Update: func(_ *Model, msg tea.Msg) (tea.Cmd, bool) {
			if _, ok := msg.(ConnMsg); ok {
				seen++
			}
			return nil, false
		},
	})
	m, _ := setup(t, 140, 20, Deps{})
	if seen == 0 {
		t.Fatal("feature Update never saw a message")
	}
	m = press(m, "x")
	if len(m.overlays) != 1 || m.overlays[0].Title() != "Test" {
		t.Fatal("feature command did not push its overlay")
	}
}

func TestGolden(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		keys []string
	}{
		{"wide_140", 140, 20, nil},
		{"wide_api_140", 140, 20, []string{"j", "j"}},
		{"narrow_list_80", 80, 20, nil},
		{"narrow_logs_80", 80, 20, []string{"enter"}},
		{"help_140", 140, 30, []string{"?"}},
		{"help_80", 80, 30, []string{"?"}},
	}
	// The golden files show the root model alone. Features test their own
	// screens, so adding one never rewrites these files.
	withFeatures(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := setup(t, tc.w, tc.h, Deps{})
			m = press(m, tc.keys...)
			got := screen(m)
			path := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got != string(want) {
				t.Errorf("%s mismatch:\n%s", path, got)
			}
		})
	}
}

func TestUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		42 * time.Second:                "42s",
		13*time.Minute + 21*time.Second: "13m 21s",
		2*time.Hour + 5*time.Minute:     "2h 5m",
	} {
		if got := uptime(d); got != want {
			t.Errorf("uptime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTabSwitchesNarrowPanes(t *testing.T) {
	m, _ := setup(t, 80, 20, Deps{})
	m = press(m, "tab")
	if s := screen(m); !strings.Contains(s, "api starting") || strings.Contains(s, "▼ backend") {
		t.Fatalf("tab did not show the log pane:\n%s", s)
	}
	m = press(m, "tab")
	if s := screen(m); !strings.Contains(s, "▼ backend") {
		t.Fatalf("tab did not return to the list:\n%s", s)
	}
}

func TestDisconnectedStatus(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	m, _ = send(m, ConnMsg{Err: errors.New("dial\x1b]0;pwned\x07 refused")})
	s := screen(m)
	if !strings.Contains(s, "● disconnected: dial") || strings.Contains(s, "13m 21s") {
		t.Fatalf("status line after a drop:\n%s", s)
	}
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("connection error reached the terminal unfiltered: %q", rest)
	}
}

func TestRenderFillsTheScreen(t *testing.T) {
	for _, size := range [][2]int{{140, 20}, {100, 12}, {99, 12}, {60, 5}, {30, 2}, {10, 1}} {
		m, _ := setup(t, size[0], size[1], Deps{Remote: true})
		for _, keys := range [][]string{nil, {"?"}, {"tab"}} {
			lines := strings.Split(screen(press(m, keys...)), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d keys %v: %d lines", size[0], size[1], keys, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%dx%d keys %v: line %d is %d wide", size[0], size[1], keys, i, w)
				}
			}
		}
	}
}

func TestViewSettings(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	v := m.View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion || v.WindowTitle != "tilt-tui localhost:10350" {
		t.Fatalf("View: AltScreen %v, MouseMode %v, WindowTitle %q", v.AltScreen, v.MouseMode, v.WindowTitle)
	}
}

func TestTriggerFailure(t *testing.T) {
	m, fc := setup(t, 140, 20, Deps{})
	fc.err = errors.New("connection refused")
	m = press(m, "j", "j", "r")
	if s := screen(m); !strings.Contains(s, "trigger failed: connection refused") {
		t.Fatalf("no failure flash:\n%s", s)
	}
}

func TestFlashExpiresOnTick(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{Flash: "startup warning"})
	clock := now
	m.d.Now = func() time.Time { return clock }
	if _, cmd := m.Update(ConnMsg{Connecting: true}); cmd != nil {
		t.Fatal("a second tick started while one was running")
	}
	clock = now.Add(9 * time.Second)
	m, _ = send(m, tickMsg(clock))
	if !strings.Contains(screen(m), "startup warning") {
		t.Fatal("startup flash gone before 10s")
	}
	clock = now.Add(10 * time.Second)
	nm, cmd := m.Update(tickMsg(clock))
	m = nm.(Model)
	if strings.Contains(screen(m), "startup warning") || !strings.Contains(screen(m), "q quit") {
		t.Fatalf("flash still shown after 10s:\n%s", screen(m))
	}
	if cmd == nil {
		t.Fatal("tick did not reschedule while the uptime is on screen")
	}
}

func TestNoTickWhileNothingChanges(t *testing.T) {
	m, _ := newModel(t, Deps{})
	m, _ = send(m, tea.WindowSizeMsg{Width: 140, Height: 20})
	m.render()
	if _, cmd := m.Update(ConnMsg{Connecting: true}); cmd != nil {
		t.Fatal("tick started with no time-based content on screen")
	}
}

func TestMouseWheelScrollsLogs(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	m.render()
	// The notch sits over the log pane: over the list it moves the cursor.
	m, _ = send(m, tea.MouseWheelMsg{X: 100, Y: 5, Button: tea.MouseWheelUp})
	if s := screen(m); strings.Contains(s, "[follow]") {
		t.Fatalf("wheel up did not leave follow mode:\n%s", s)
	}
}

func TestOlderFlashDoesNotReplaceNewer(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	m, _ = send(m, flashMsg{text: "triggered api", at: now})
	m, _ = send(m, flashMsg{text: "triggering api…", at: now.Add(-time.Second)})
	if s := screen(m); !strings.Contains(s, "triggered api") {
		t.Fatalf("a stale flash replaced a newer one:\n%s", s)
	}
}

// checkFrame fails unless the frame is exactly h rows of at most w cells.
func checkFrame(t *testing.T, m Model, w, h int) {
	t.Helper()
	lines := strings.Split(screen(m), "\n")
	if len(lines) != h {
		t.Errorf("%dx%d: %d lines", w, h, len(lines))
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			t.Errorf("%dx%d: line %d is %d wide", w, h, i, lw)
		}
	}
}

func TestNarrowLogFooterKeepsMarkerAndQuit(t *testing.T) {
	m, _ := setup(t, 80, 20, Deps{Remote: true})
	m = press(m, "enter")
	m, _ = send(m, ConnMsg{Err: errors.New("get websocket token: " + strings.Repeat("x", 179))})
	lines := strings.Split(screen(m), "\n")
	footer := lines[len(lines)-1]
	for _, want := range []string{"⚠ unencrypted remote", "q quit", "● disconnected"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer lacks %q: %q", want, footer)
		}
	}
	checkFrame(t, m, 80, 20)
}

func TestListPaneKeepsMarkerWhenShort(t *testing.T) {
	for _, size := range [][2]int{{140, 4}, {80, 4}, {80, 5}} {
		m, _ := setup(t, size[0], size[1], Deps{Remote: true})
		m, _ = send(m, ConnMsg{Err: errors.New("dial tcp: connection refused")})
		if s := screen(m); !strings.Contains(s, "⚠ unencrypted remote") {
			t.Errorf("%dx%d: marker dropped:\n%s", size[0], size[1], s)
		}
		checkFrame(t, m, size[0], size[1])
	}
}

func TestTickStartsWhenViewBringsRelativeTimes(t *testing.T) {
	m, _ := newModel(t, Deps{})
	m, _ = send(m, tea.WindowSizeMsg{Width: 140, Height: 20})
	m, _ = send(m, ConnMsg{Connecting: true})
	v := snapshot(t, true)
	v.TiltStartTime = time.Time{}
	if _, cmd := m.Update(v); cmd == nil {
		t.Fatal("no tick after a view put build times on screen")
	}
}

func TestFeatureHandledStopsHandling(t *testing.T) {
	saved := features
	t.Cleanup(func() { features = saved })
	register(Feature{
		Name: "eats-j",
		Update: func(_ *Model, msg tea.Msg) (tea.Cmd, bool) {
			k, ok := msg.(tea.KeyPressMsg)
			return nil, ok && k.String() == "j"
		},
	})
	m, _ := setup(t, 140, 20, Deps{})
	before := m.list.Selected()
	m = press(m, "j", "j")
	if m.list.Selected() != before {
		t.Fatal("a key a feature handled still moved the list")
	}
}

func hookCommand(id command.ID, k string) command.Command {
	return command.Command{ID: id, Title: string(id), Keys: []string{k}, Scope: command.ScopeGlobal,
		Run: func(command.Context) tea.Cmd { return nil }}
}

func TestFeatureHooks(t *testing.T) {
	panel := false
	withFeatures(t, Feature{
		Name:        "hooks",
		Commands:    func(*Model) []command.Command { return []command.Command{hookCommand("hook", "x")} },
		StatusParts: func(*Model) []string { return []string{"extra\x1b]0;pwned\x07part", ""} },
		ListTitle:   func(*Model) string { return "filter: errors" },
		LogTitle:    func(*Model) string { return "3/17" },
		Hints:       []Hint{{ID: "hook", Label: "hook"}, {ID: "missing", Label: "never shown"}},
		Panel: func(*Model) (string, string, bool) {
			return "tilt exited (code 1)", "first\nerror: bad\x1b]52;c;ZXZpbA==\x07 Tiltfile", panel
		},
	})
	m, _ := setup(t, 140, 20, Deps{})
	s := screen(m)
	for _, want := range []string{
		"✓ 2/3  filter: errors",
		"[follow]  3/17",
		"│extrapart ",
		"r trigger · x hook · tab switch · ? help · q quit",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "never shown") {
		t.Error("a hint for a missing command was shown")
	}

	panel = true
	s = screen(m)
	for _, want := range []string{"tilt exited (code 1)", "first", "error: bad Tiltfile"} {
		if !strings.Contains(s, want) {
			t.Errorf("panel lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "api starting") {
		t.Errorf("log lines drawn under the panel:\n%s", s)
	}
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("hook text reached the terminal unfiltered: %q", rest)
	}
}

func TestFeatureConnStatus(t *testing.T) {
	starting := true
	withFeatures(t,
		Feature{Name: "off", ConnStatus: func(*Model) (string, bool) { return "never", false }},
		Feature{Name: "child", ConnStatus: func(*Model) (string, bool) { return "starting tilt…", starting }},
		Feature{Name: "late", ConnStatus: func(*Model) (string, bool) { return "too late", true }},
	)
	m, _ := setup(t, 140, 20, Deps{})
	m, _ = send(m, ConnMsg{Err: errors.New("dial tcp: connection refused")})
	s := screen(m)
	if !strings.Contains(s, "starting tilt…  localhost:10350") || strings.Contains(s, "disconnected") {
		t.Fatalf("ConnStatus did not replace the connection state:\n%s", s)
	}
	if strings.Contains(s, "never") || strings.Contains(s, "too late") {
		t.Fatalf("a ConnStatus other than the first ok one was drawn:\n%s", s)
	}
	starting = false
	if s := screen(m); !strings.Contains(s, "too late") {
		t.Fatalf("the next ok ConnStatus did not take over:\n%s", s)
	}
}

func TestFeatureHintsDropWhenFooterIsFull(t *testing.T) {
	var cmds []command.Command
	var hints []Hint
	for _, k := range []string{"a", "b", "c", "x", "y", "z"} {
		id := command.ID("hint-" + k)
		cmds = append(cmds, hookCommand(id, k))
		hints = append(hints, Hint{ID: id, Label: "long feature hint " + k})
	}
	withFeatures(t, Feature{
		Name:     "many-hints",
		Commands: func(*Model) []command.Command { return cmds },
		Hints:    hints,
	})
	m, _ := setup(t, 80, 20, Deps{Remote: true})
	footer := func(m Model) string {
		lines := strings.Split(screen(m), "\n")
		return lines[len(lines)-1]
	}
	f := footer(m)
	if !strings.Contains(f, "r trigger · a long feature hint a") || !strings.HasSuffix(f, "? help · q quit") {
		t.Fatalf("footer = %q, want the first hints and every core hint", f)
	}
	if strings.Contains(f, "hint z") {
		t.Fatalf("footer = %q, the last hint should have been dropped", f)
	}
	m = press(m, "enter")
	if f := footer(m); !strings.Contains(f, "⚠ unencrypted remote") || !strings.HasSuffix(f, "q quit") {
		t.Fatalf("narrow log footer = %q, want the marker and q quit", f)
	}
	checkFrame(t, m, 80, 20)
}

func TestFlashCmdDuration(t *testing.T) {
	m, _ := setup(t, 140, 20, Deps{})
	clock := now
	m.d.Now = func() time.Time { return clock }
	m, _ = send(m, m.flashCmd("disabled api · ctrl+d again to re-enable", 5*time.Second)())
	clock = now.Add(4 * time.Second)
	m, _ = send(m, tickMsg(clock))
	if !strings.Contains(screen(m), "disabled api") {
		t.Fatal("5s flash gone after 4s")
	}
	clock = now.Add(5 * time.Second)
	m, _ = send(m, tickMsg(clock))
	if strings.Contains(screen(m), "disabled api") {
		t.Fatal("5s flash still shown after 5s")
	}

	m, _ = send(m, m.flashCmd("default length", 0)())
	clock = clock.Add(3 * time.Second)
	m, _ = send(m, tickMsg(clock))
	if strings.Contains(screen(m), "default length") {
		t.Fatal("a zero duration did not mean 3s")
	}
}

func TestDepsOpen(t *testing.T) {
	m, _ := newModel(t, Deps{})
	if err := m.d.Open(context.Background(), "http://localhost:8080"); !errors.Is(err, errNoOpener) {
		t.Fatalf("default Open = %v, want errNoOpener", err)
	}
	var opened []string
	m, _ = newModel(t, Deps{Open: func(_ context.Context, u string) error { opened = append(opened, u); return nil }})
	if err := m.d.Open(context.Background(), "http://localhost:8080"); err != nil || !slices.Equal(opened, []string{"http://localhost:8080"}) {
		t.Fatalf("Open = %v, opened %q", err, opened)
	}
}

func TestFeatureStatePerModel(t *testing.T) {
	type counter struct{ n int }
	newCounter := func() *counter { return &counter{} }
	a, _ := newModel(t, Deps{})
	b, _ := newModel(t, Deps{})
	featureState(&a, "count", newCounter).n++
	featureState(&a, "count", newCounter).n++
	if got := featureState(&a, "count", newCounter).n; got != 2 {
		t.Fatalf("a's state = %d, want 2", got)
	}
	if got := featureState(&b, "count", newCounter).n; got != 0 {
		t.Fatalf("b shares a's state: %d", got)
	}
	nm, _ := a.Update(ConnMsg{Connecting: true})
	if got := featureState(new(nm.(Model)), "count", newCounter).n; got != 2 {
		t.Fatalf("state lost across Update: %d", got)
	}
}
