package ui

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// --- Contract assertions ---

var (
	_ func(*Model, tilt.Resource) tea.Cmd   = (*Model).pressToggle
	_ func(*Model, string, bool) tea.Cmd    = (*Model).clickToggle
	_ func(*Model, tea.Msg) (tea.Cmd, bool) = updateToggle
	_ func(*Model) []string                 = togglePendingParts
	_ func(tilt.Button) string              = toggleAction
	_ overlay.Overlay                       = (*toggleConfirm)(nil)
	_ time.Duration                         = togglePendingFor
	_ time.Duration                         = toggleDoneFor
)

// --- Helpers ---

var toggleCtrlD = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}

// toggleButton is api's disable toggle as Tilt sends it: the recorded
// fixture's shape, with action "on" while api is enabled.
func toggleButton(rv, action string, confirm bool) tilt.Button {
	return tilt.Button{
		Metadata: tilt.ObjectMeta{
			Name:            "toggle-api-disable",
			ResourceVersion: rv,
			Annotations:     map[string]string{tilt.AnnotationButtonType: tilt.ButtonTypeDisableToggle},
		},
		Spec: tilt.ButtonSpec{
			Location:             &tilt.ButtonLocation{ComponentID: "api", ComponentType: tilt.ComponentTypeResource},
			Text:                 "Disable Resource",
			RequiresConfirmation: confirm,
			Inputs:               []tilt.InputSpec{{Name: "action", Hidden: &tilt.HiddenInputSpec{Value: action}}},
		},
	}
}

// disabledAPI is a resource update that marks api disabled.
func disabledAPI() ViewMsg {
	return ViewMsg{UIResources: []tilt.Resource{{
		Metadata: tilt.ObjectMeta{Name: "api", Labels: map[string]string{"app": "backend"}},
		Status: tilt.ResourceStatus{Order: 2, DisableStatus: &tilt.DisableStatus{
			DisabledCount: 1, State: "Disabled",
		}},
	}}}
}

// toggleSend delivers msg and feeds back what its commands produce, up
// to four levels deep: key, y/n answer, click result and flash.
func toggleSend(m Model, msg tea.Msg) Model {
	queue := []tea.Msg{msg}
	for range 4 {
		var next []tea.Msg
		for _, q := range queue {
			nm, cmd := m.Update(q)
			m = nm.(Model)
			for _, r := range drain(cmd) {
				if _, quit := r.(tea.QuitMsg); !quit {
					next = append(next, r)
				}
			}
		}
		queue = next
	}
	return m
}

// toggleSetup returns the setup model with only the disable feature, a
// clock the test moves, api's toggle in the store and the cursor on api.
func toggleSetup(t *testing.T, w, h int, b tilt.Button) (Model, *fakeClient, *time.Time) {
	t.Helper()
	var f Feature
	for _, x := range features {
		if x.Name == "disable" {
			f = x
		}
	}
	if f.Name == "" {
		t.Fatal("disable feature not registered")
	}
	withFeatures(t, f)
	m, fc := setup(t, w, h, Deps{})
	clock := now
	m.d.Now = func() time.Time { return clock }
	m, _ = send(m, ViewMsg{UIButtons: []tilt.Button{b}})
	m = press(m, "j", "j")
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatal("cursor is not on api")
	}
	return m, fc, &clock
}

func togglePendingAPI(m Model) bool {
	_, ok := m.toggles().pending["api"]
	return ok
}

// --- Behavior tests ---

func TestToggleAsksWhenConfirmationRequired(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
	m = toggleSend(m, toggleCtrlD)
	if s := screen(m); !strings.Contains(s, "disable api? y/n") {
		t.Fatalf("no confirmation:\n%s", s)
	}
	if len(fc.clicks) != 0 {
		t.Fatal("clicked before the answer")
	}
	m = toggleSend(m, toggleCtrlD) // the overlay takes the key
	m = toggleSend(m, key("y"))
	if len(fc.clicks) != 1 || !maps.Equal(fc.clicks[0].values, map[string]any{"action": "on"}) {
		t.Fatalf("clicks = %#v, want one with action on", fc.clicks)
	}
	if fc.clicks[0].button.Metadata.ResourceVersion != "19" {
		t.Fatalf("clicked version %s", fc.clicks[0].button.Metadata.ResourceVersion)
	}
	if len(m.overlays) != 0 {
		t.Fatal("confirmation still open")
	}
}

func TestToggleCancelSendsNothing(t *testing.T) {
	for _, k := range []string{"n", "esc", "q"} {
		m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
		m = toggleSend(m, toggleCtrlD)
		m = toggleSend(m, key(k))
		if len(fc.clicks) != 0 || len(m.overlays) != 0 || togglePendingAPI(m) {
			t.Fatalf("%s: %d clicks, %d overlays, pending %v", k, len(fc.clicks), len(m.overlays), togglePendingAPI(m))
		}
		// A cancelled question doesn't block the next ctrl+d.
		m = toggleSend(m, toggleCtrlD)
		if !strings.Contains(screen(m), "disable api? y/n") {
			t.Fatalf("%s: ctrl+d after a cancel did not ask again", k)
		}
	}
}

func TestToggleAsksEnableWhenDisabled(t *testing.T) {
	b := toggleButton("30", "off", true)
	b.Spec.Text = "Enable Resource"
	m, fc, _ := toggleSetup(t, 140, 20, b)
	m, _ = send(m, disabledAPI())
	m = press(m, "d") // show disabled resources
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("cursor left api:\n%s", screen(m))
	}
	m = toggleSend(m, toggleCtrlD)
	if s := screen(m); !strings.Contains(s, "enable api? y/n") {
		t.Fatalf("no enable question:\n%s", s)
	}
	m = toggleSend(m, key("y"))
	if len(fc.clicks) != 1 || !maps.Equal(fc.clicks[0].values, map[string]any{"action": "off"}) {
		t.Fatalf("clicks = %#v", fc.clicks)
	}
	if s := screen(m); !strings.Contains(s, "enabling api…") || !strings.Contains(s, "enabled api · ctrl+d again to disable") {
		t.Fatalf("no enable status:\n%s", s)
	}
}

func TestToggleWithoutConfirmationClicksAtOnce(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 1 || len(m.overlays) != 0 {
		t.Fatalf("%d clicks, %d overlays", len(fc.clicks), len(m.overlays))
	}
}

// Criterion 23: presses while pending send nothing, a resource update
// doesn't clear the toggle, and the changed action does.
func TestTogglePendingUntilActionChanges(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
	m = toggleSend(m, toggleCtrlD)
	m = toggleSend(m, key("y"))
	if !togglePendingAPI(m) {
		t.Fatal("not pending after the click")
	}
	if s := screen(m); !strings.Contains(s, "disabling api…") {
		t.Fatalf("no pending status:\n%s", s)
	}

	for range 3 {
		m = toggleSend(m, toggleCtrlD)
	}
	if len(fc.clicks) != 1 || len(m.overlays) != 0 {
		t.Fatalf("ctrl+d while pending: %d clicks, %d overlays", len(fc.clicks), len(m.overlays))
	}

	// The resource update can arrive before the button.
	m, _ = send(m, disabledAPI())
	if !togglePendingAPI(m) {
		t.Fatal("a resource disable-state update cleared the pending toggle")
	}
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 1 {
		t.Fatal("ctrl+d after the resource update clicked again")
	}

	// An update of the same button with the same action keeps it pending.
	m, _ = send(m, ViewMsg{UIButtons: []tilt.Button{toggleButton("21", "on", true)}})
	if !togglePendingAPI(m) {
		t.Fatal("a button update with the same action cleared the toggle")
	}

	flipped := toggleButton("22", "off", true)
	flipped.Spec.Text = "Enable Resource"
	m, _ = send(m, ViewMsg{UIButtons: []tilt.Button{flipped}})
	if togglePendingAPI(m) {
		t.Fatal("the changed action did not clear the toggle")
	}
	if s := screen(m); strings.Contains(s, "disabling api…") {
		t.Fatalf("pending status still drawn:\n%s", s)
	}
}

// Criterion 23: ten seconds clear the toggle.
func TestTogglePendingExpires(t *testing.T) {
	m, fc, clock := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	m = toggleSend(m, toggleCtrlD)
	*clock = now.Add(9 * time.Second)
	m = toggleSend(m, toggleExpireMsg{})
	m = toggleSend(m, tickMsg(*clock))
	if !togglePendingAPI(m) {
		t.Fatal("pending cleared before 10s")
	}
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 1 {
		t.Fatal("ctrl+d at 9s clicked again")
	}

	*clock = now.Add(togglePendingFor)
	m = toggleSend(m, toggleExpireMsg{})
	if togglePendingAPI(m) {
		t.Fatal("pending not cleared at 10s")
	}
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 2 {
		t.Fatalf("%d clicks after expiry, want 2", len(fc.clicks))
	}

	// The root tick also clears it.
	*clock = clock.Add(togglePendingFor)
	m = toggleSend(m, tickMsg(*clock))
	if togglePendingAPI(m) {
		t.Fatal("tick did not clear an expired toggle")
	}
}

func TestToggleConflict(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	fc.clickErr = tilt.ErrConflict
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 1 {
		t.Fatalf("%d clicks after a 409, want 1", len(fc.clicks))
	}
	if s := screen(m); !strings.Contains(s, "button changed, try again") {
		t.Fatalf("no conflict flash:\n%s", s)
	}
	if togglePendingAPI(m) {
		t.Fatal("a 409 left the toggle pending")
	}

	fc.clickErr = errors.New("refused\x1b]0;pwned\x07")
	m = toggleSend(m, toggleCtrlD)
	if s := screen(m); !strings.Contains(s, "toggle failed: refused") || togglePendingAPI(m) {
		t.Fatalf("failure: pending %v\n%s", togglePendingAPI(m), s)
	}
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("error text reached the terminal unfiltered: %q", rest)
	}
}

func TestToggleFooterAfterSuccess(t *testing.T) {
	m, _, clock := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	m = toggleSend(m, toggleCtrlD)
	footer := func() string {
		lines := strings.Split(screen(m), "\n")
		return lines[len(lines)-1]
	}
	if f := footer(); !strings.Contains(f, "disabled api · ctrl+d again to re-enable") {
		t.Fatalf("footer = %q", f)
	}
	*clock = now.Add(4 * time.Second)
	m = toggleSend(m, tickMsg(*clock))
	if f := footer(); !strings.Contains(f, "disabled api") {
		t.Fatalf("result gone after 4s: %q", f)
	}
	*clock = now.Add(toggleDoneFor)
	m = toggleSend(m, tickMsg(*clock))
	if f := footer(); strings.Contains(f, "disabled api") {
		t.Fatalf("result still shown after 5s: %q", f)
	}
}

func TestToggleKeepsReturnedButton(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	fc.clickResult = func(b tilt.Button) tilt.Button {
		b.Metadata.ResourceVersion = "20"
		return b
	}
	m = toggleSend(m, toggleCtrlD)
	if b, _ := m.store.DisableButton("api"); b.Metadata.ResourceVersion != "20" {
		t.Fatalf("store holds version %s, want the returned 20", b.Metadata.ResourceVersion)
	}
	if !togglePendingAPI(m) {
		t.Fatal("storing the returned button, same action, cleared the toggle")
	}

	// A newer button from the websocket wins over a late result.
	m, _ = send(m, ViewMsg{UIButtons: []tilt.Button{toggleButton("25", "on", false)}})
	m, _ = send(m, toggleResultMsg{seq: 99, resource: "api", disable: true,
		sent: toggleButton("20", "on", false), got: toggleButton("21", "on", false), at: now})
	if b, _ := m.store.DisableButton("api"); b.Metadata.ResourceVersion != "25" {
		t.Fatalf("store holds version %s, want the websocket's 25", b.Metadata.ResourceVersion)
	}
}

func TestToggleStaleFailureKeepsNewPending(t *testing.T) {
	m, _, clock := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	m = toggleSend(m, toggleCtrlD)
	first := m.toggles().pending["api"].seq
	*clock = now.Add(togglePendingFor)
	m = toggleSend(m, toggleExpireMsg{})
	m = toggleSend(m, toggleCtrlD)
	m, _ = send(m, toggleResultMsg{seq: first, resource: "api", disable: true, err: tilt.ErrConflict, at: *clock})
	if !togglePendingAPI(m) {
		t.Fatal("a late failure of the first click cleared the second")
	}
}

func TestToggleIgnoresOtherResources(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	m = press(m, "k", "k") // All Resources
	m = toggleSend(m, toggleCtrlD)
	m = press(m, "j", "j", "j", "j") // ▼ frontend, web
	m = toggleSend(m, toggleCtrlD)
	if len(fc.clicks) != 0 {
		t.Fatalf("%d clicks without a toggle", len(fc.clicks))
	}
	if s := screen(m); !strings.Contains(s, "has no disable toggle") {
		t.Fatalf("no flash for a resource without a toggle:\n%s", s)
	}
}

func TestTogglePendingIsPerModel(t *testing.T) {
	a, fa, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	b, fb, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", false))
	a = toggleSend(a, toggleCtrlD)
	if !togglePendingAPI(a) || len(fa.clicks) != 1 {
		t.Fatal("model a did not start a pending toggle")
	}
	if togglePendingAPI(b) {
		t.Fatal("model b sees model a's pending toggle")
	}
	b = toggleSend(b, toggleCtrlD)
	if len(fb.clicks) != 1 {
		t.Fatalf("model b sent %d clicks, want 1", len(fb.clicks))
	}
	if s := screen(b); !strings.Contains(s, "disabling api…") {
		t.Fatalf("model b has no pending status:\n%s", s)
	}
}

func TestToggleGolden(t *testing.T) {
	m, _, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
	m = toggleSend(m, toggleCtrlD)
	got := screen(m)
	checkFrame(t, m, 140, 20)
	path := filepath.Join("testdata", "disable_confirm_140.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -run TestToggleGolden -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch:\n%s", path, got)
	}
}

// toggleSkipped checks that ctrl+d sent no click, flashed the changed
// text and left api's toggle free for the next ctrl+d.
func toggleSkipped(t *testing.T, m Model, fc *fakeClient) {
	t.Helper()
	if len(fc.clicks) != 0 {
		t.Fatalf("clicks = %#v, want none", fc.clicks)
	}
	if s := screen(m); !strings.Contains(s, "api changed, try again") {
		t.Fatalf("no changed flash:\n%s", s)
	}
	if togglePendingAPI(m) || len(m.overlays) != 0 {
		t.Fatalf("pending %v, %d overlays", togglePendingAPI(m), len(m.overlays))
	}
}

// An enabled resource whose toggle holds action "off" would be enabled
// by a click, not disabled.
func TestToggleSkipsClickWhenActionDisagrees(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "off", true))
	m = toggleSend(m, toggleCtrlD)
	if s := screen(m); !strings.Contains(s, "disable api? y/n") {
		t.Fatalf("no confirmation:\n%s", s)
	}
	m = toggleSend(m, key("y"))
	toggleSkipped(t, m, fc)
	m = toggleSend(m, toggleCtrlD)
	if !strings.Contains(screen(m), "disable api? y/n") {
		t.Fatal("ctrl+d after a skipped click did not ask again")
	}
}

func TestToggleSkipsClickWhenActionDisagreesWithoutConfirmation(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "off", false))
	m = toggleSend(m, toggleCtrlD)
	toggleSkipped(t, m, fc)
}

// Tilt's web UI disables api while the question is open.
func TestToggleSkipsClickWhenResourceFlippedDuringQuestion(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
	m = toggleSend(m, toggleCtrlD)
	if s := screen(m); !strings.Contains(s, "disable api? y/n") {
		t.Fatalf("no confirmation:\n%s", s)
	}
	flip := disabledAPI()
	flip.UIButtons = []tilt.Button{toggleButton("22", "off", true)}
	m, _ = send(m, flip)
	m = toggleSend(m, key("y"))
	toggleSkipped(t, m, fc)
}

// The resource can show disabled before Tilt flips the toggle's action.
func TestToggleSkipsClickWhenOnlyResourceFlipped(t *testing.T) {
	m, fc, _ := toggleSetup(t, 140, 20, toggleButton("19", "on", true))
	m = toggleSend(m, toggleCtrlD)
	m, _ = send(m, disabledAPI())
	m = toggleSend(m, key("y"))
	toggleSkipped(t, m, fc)
}
