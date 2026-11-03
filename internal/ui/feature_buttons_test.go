package ui

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// --- Contract assertions ---

var (
	_ func(*Model, string) tea.Cmd                      = (*Model).openButtons
	_ func(*tilt.Store, tilt.Button, tilt.Button)       = keepClickedButton
	_ func(*Model, tilt.Button, map[string]any) tea.Cmd = (*Model).clickButton
	_ func(*Model, tea.Msg) (tea.Cmd, bool)             = updateButtons
	_                                                   = buttonResultMsg{sent: tilt.Button{}, got: tilt.Button{}, err: nil}
	_ string                                            = buttonConflictText
)

// --- Helpers ---

// buttonsKey builds the key press whose String() is s. It covers the
// keys the root key helper doesn't.
func buttonsKey(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	}
	return key(s)
}

// buttonsSend delivers msg and feeds back the messages its commands
// produce, up to four levels deep. A key in the form produces ClickMsg,
// the click command produces the result, and the result shows a flash.
func buttonsSend(m Model, msg tea.Msg) Model {
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

func buttonsPress(m Model, keys ...string) Model {
	for _, k := range keys {
		m = buttonsSend(m, buttonsKey(k))
	}
	return m
}

func buttonsAPI(name, rv string, spec tilt.ButtonSpec) tilt.Button {
	spec.Location = &tilt.ButtonLocation{ComponentID: "api", ComponentType: tilt.ComponentTypeResource}
	return tilt.Button{Metadata: tilt.ObjectMeta{Name: name, ResourceVersion: rv}, Spec: spec}
}

// buttonsSetup returns the setup model with bs added for api and the
// cursor on api.
func buttonsSetup(t *testing.T, w, h int, bs ...tilt.Button) (Model, *fakeClient) {
	t.Helper()
	withFeatures(t, buttonsFeature(t))
	m, fc := setup(t, w, h, Deps{})
	m, _ = send(m, ViewMsg{UIButtons: bs})
	m = press(m, "j", "j")
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatal("cursor is not on api")
	}
	return m, fc
}

// buttonsFeature returns the feature this file registers, so a test runs
// it alone.
func buttonsFeature(t *testing.T) Feature {
	t.Helper()
	for _, f := range features {
		if f.Name == "buttons" {
			return f
		}
	}
	t.Fatal("buttons feature not registered")
	return Feature{}
}

var (
	deployText   = tilt.InputSpec{Name: "msg", Label: "Message", Text: &tilt.TextInputSpec{DefaultValue: "v1"}}
	deployBool   = tilt.InputSpec{Name: "force", Label: "Force", Bool: &tilt.BoolInputSpec{}}
	deployChoice = tilt.InputSpec{Name: "env", Label: "Env", Choice: &tilt.ChoiceInputSpec{Choices: []string{"dev", "staging", "prod"}}}
	deployHidden = tilt.InputSpec{Name: "token", Hidden: &tilt.HiddenInputSpec{Value: "s3cret"}}
)

// --- Behavior tests ---

func TestButtonsSendsEachInputType(t *testing.T) {
	cases := []struct {
		name  string
		input tilt.InputSpec
		keys  []string
		want  map[string]any
	}{
		{"text", deployText, []string{"x", "space", "y"}, map[string]any{"msg": "v1x y"}},
		{"bool", deployBool, []string{"space"}, map[string]any{"force": true}},
		{"choice", deployChoice, []string{"right", "j"}, map[string]any{"env": "prod"}},
		{"choice back", deployChoice, []string{"left", "k"}, map[string]any{"env": "staging"}},
		{"hidden", deployHidden, nil, map[string]any{"token": "s3cret"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy", Inputs: []tilt.InputSpec{tc.input}})
			m, fc := buttonsSetup(t, 140, 20, b)
			m = buttonsPress(m, "b", "enter")
			m = buttonsPress(m, tc.keys...)
			m = buttonsPress(m, "enter")
			if len(fc.clicks) != 1 {
				t.Fatalf("%d clicks, want 1", len(fc.clicks))
			}
			c := fc.clicks[0]
			if !maps.Equal(c.values, tc.want) {
				t.Fatalf("values = %#v, want %#v", c.values, tc.want)
			}
			if c.button.Metadata.Name != "api-deploy" || c.button.Metadata.ResourceVersion != "7" {
				t.Fatalf("clicked %s at version %s", c.button.Metadata.Name, c.button.Metadata.ResourceVersion)
			}
			if len(m.overlays) != 0 {
				t.Fatal("overlay still open after submit")
			}
			if s := screen(m); !strings.Contains(s, "clicked Deploy") {
				t.Fatalf("no result flash:\n%s", s)
			}
		})
	}
}

func TestButtonsAllInputsInOneForm(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy",
		Inputs: []tilt.InputSpec{deployText, deployHidden, deployBool, deployChoice}})
	m, fc := buttonsSetup(t, 140, 20, b)
	buttonsPress(m, "b", "enter", "2", "tab", "space", "tab", "right", "right", "enter")
	want := map[string]any{"msg": "v12", "token": "s3cret", "force": true, "env": "prod"}
	if len(fc.clicks) != 1 || !maps.Equal(fc.clicks[0].values, want) {
		t.Fatalf("clicks = %#v, want values %#v", fc.clicks, want)
	}
}

func TestButtonsListsStopBuild(t *testing.T) {
	stop := buttonsAPI("api-stopbuild", "3", tilt.ButtonSpec{Text: "Stop Build"})
	stop.Metadata.Annotations = map[string]string{tilt.AnnotationButtonType: "StopBuild"}
	toggle := buttonsAPI("toggle-api-disable", "4", tilt.ButtonSpec{Text: "Disable Resource",
		Inputs: []tilt.InputSpec{{Name: "action", Hidden: &tilt.HiddenInputSpec{Value: "on"}}}})
	toggle.Metadata.Annotations = map[string]string{tilt.AnnotationButtonType: tilt.ButtonTypeDisableToggle}
	m, fc := buttonsSetup(t, 140, 20, stop, toggle)
	m = buttonsPress(m, "b")
	s := screen(m)
	if !strings.Contains(s, "Buttons: api") || !strings.Contains(s, "› Stop Build") {
		t.Fatalf("Stop Build not listed:\n%s", s)
	}
	if strings.Contains(s, "Disable Resource") {
		t.Fatalf("the disable toggle is listed:\n%s", s)
	}
	m = buttonsPress(m, "enter")
	if len(fc.clicks) != 1 || len(fc.clicks[0].values) != 0 {
		t.Fatalf("clicks = %#v, want one click with no values", fc.clicks)
	}
}

func TestButtonsCancelSendsNothing(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy", RequiresConfirmation: true,
		Inputs: []tilt.InputSpec{deployBool}})
	for _, keys := range [][]string{
		{"b", "esc"},
		{"b", "enter", "esc"},
		{"b", "enter", "space", "enter", "n"},
		{"b", "enter", "space", "enter", "esc"},
	} {
		m, fc := buttonsSetup(t, 140, 20, b)
		m = buttonsPress(m, keys...)
		if len(fc.clicks) != 0 {
			t.Fatalf("keys %v sent %d clicks", keys, len(fc.clicks))
		}
		if len(m.overlays) != 0 {
			t.Fatalf("keys %v left an overlay open", keys)
		}
	}

	m, fc := buttonsSetup(t, 140, 20, b)
	m = buttonsPress(m, "b", "enter", "space", "enter")
	if s := screen(m); !strings.Contains(s, "Deploy? y/n") {
		t.Fatalf("no y/n step:\n%s", s)
	}
	m = buttonsPress(m, "y")
	if len(fc.clicks) != 1 || !maps.Equal(fc.clicks[0].values, map[string]any{"force": true}) {
		t.Fatalf("y gave clicks %#v", fc.clicks)
	}
}

func TestButtonsConflictDoesNotRetry(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy"})
	m, fc := buttonsSetup(t, 140, 20, b)
	fc.clickErr = tilt.ErrConflict
	m = buttonsPress(m, "b", "enter")
	if len(fc.clicks) != 1 {
		t.Fatalf("%d clicks after a 409, want 1", len(fc.clicks))
	}
	if s := screen(m); !strings.Contains(s, "button changed, try again") {
		t.Fatalf("no conflict flash:\n%s", s)
	}
	if got, _ := m.store.Button("api-deploy"); got.Metadata.ResourceVersion != "7" {
		t.Fatalf("store changed after a 409: version %s", got.Metadata.ResourceVersion)
	}

	fc.clickErr = errors.New("boom\x1b]0;pwned\x07")
	m = buttonsPress(m, "b", "enter")
	if s := screen(m); !strings.Contains(s, "button failed: boom") {
		t.Fatalf("no failure flash:\n%s", s)
	}
}

func TestButtonsDisabledIsDimmedAndIgnored(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy", Disabled: true})
	m, fc := buttonsSetup(t, 140, 20, b)
	m = buttonsPress(m, "b", "enter")
	if len(fc.clicks) != 0 || len(m.overlays) != 1 {
		t.Fatalf("enter on a disabled button: %d clicks, %d overlays", len(fc.clicks), len(m.overlays))
	}
	if !strings.Contains(m.render(), m.d.Theme.Dim.Render("› Deploy (disabled)")) {
		t.Fatalf("disabled button not dimmed:\n%s", screen(m))
	}
}

func TestButtonsNoButtonsFlashes(t *testing.T) {
	m, _ := buttonsSetup(t, 140, 20)
	m = buttonsPress(m, "b")
	if len(m.overlays) != 0 || !strings.Contains(screen(m), "api has no buttons") {
		t.Fatalf("b without buttons:\n%s", screen(m))
	}
	m = press(m, "k", "k") // All Resources
	m = buttonsPress(m, "b")
	if len(m.overlays) != 0 {
		t.Fatal("b opened an overlay with All Resources selected")
	}
}

func TestButtonsKeepsReturnedButton(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy"})
	m, fc := buttonsSetup(t, 140, 20, b)
	fc.clickResult = func(b tilt.Button) tilt.Button {
		b.Metadata.ResourceVersion = "8"
		return b
	}
	m = buttonsPress(m, "b", "enter")
	if got, _ := m.store.Button("api-deploy"); got.Metadata.ResourceVersion != "8" {
		t.Fatalf("store holds version %s, want the returned 8", got.Metadata.ResourceVersion)
	}

	// A newer button arrived over the websocket before the result.
	newer := buttonsAPI("api-deploy", "9", tilt.ButtonSpec{Text: "Deploy"})
	m, _ = send(m, ViewMsg{UIButtons: []tilt.Button{newer}})
	got8 := buttonsAPI("api-deploy", "10", tilt.ButtonSpec{Text: "Deploy"})
	m, _ = send(m, buttonResultMsg{sent: buttonsAPI("api-deploy", "8", tilt.ButtonSpec{}), got: got8, at: now})
	if got, _ := m.store.Button("api-deploy"); got.Metadata.ResourceVersion != "9" {
		t.Fatalf("store holds version %s, want the websocket's 9", got.Metadata.ResourceVersion)
	}
}

func TestButtonsFilterTiltText(t *testing.T) {
	evil := "\x1b]52;c;ZXZpbA==\x07"
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy" + evil, RequiresConfirmation: true,
		Inputs: []tilt.InputSpec{
			{Name: "msg", Label: "Message" + evil, Text: &tilt.TextInputSpec{DefaultValue: "v1" + evil}},
			{Name: "force", Label: "Force" + evil, Bool: &tilt.BoolInputSpec{}},
			{Name: "env", Label: "Env" + evil, Choice: &tilt.ChoiceInputSpec{Choices: []string{"dev" + evil}}},
		}})
	m, _ := buttonsSetup(t, 140, 20, b)
	for _, keys := range [][]string{{"b"}, {"enter"}, {"enter"}} {
		m = buttonsPress(m, keys...)
		if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07") {
			t.Fatalf("keys %v: escape sequence reached the terminal: %q", keys, rest)
		}
	}
	if s := screen(m); !strings.Contains(s, "Deploy? y/n") {
		t.Fatalf("y/n step missing:\n%s", s)
	}
}

func TestButtonsHint(t *testing.T) {
	m, _ := buttonsSetup(t, 140, 20)
	lines := strings.Split(screen(m), "\n")
	if f := lines[len(lines)-1]; !strings.Contains(f, "r trigger · b buttons · tab switch") {
		t.Fatalf("footer = %q", f)
	}
}

func TestButtonsGolden(t *testing.T) {
	b := buttonsAPI("api-deploy", "7", tilt.ButtonSpec{Text: "Deploy",
		Inputs: []tilt.InputSpec{deployText, deployHidden, deployBool, deployChoice}})
	m, _ := buttonsSetup(t, 140, 20, b)
	m = buttonsPress(m, "b", "enter", "tab", "space")
	got := screen(m)
	checkFrame(t, m, 140, 20)
	path := filepath.Join("testdata", "buttons_form_140.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -run TestButtonsGolden -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch:\n%s", path, got)
	}
}
