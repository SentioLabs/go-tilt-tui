package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// restartModel is a live model with the child and restart features.
func restartModel(t *testing.T) Model {
	t.Helper()
	withFeatures(t, childFeature(), restartFeature())
	m, _ := setup(t, 140, 20, Deps{})
	return m
}

// flatten runs cmd and returns the messages it produces in order. It
// expands a batch or a sequence into its commands' messages.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flatten(c)...)
		}
		return out
	}
	// tea.Sequence returns an unexported []tea.Cmd type.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(cmd) {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, flatten(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// pressRMsgs presses R and returns the messages its command produces in
// order, after feeding each back to the model.
func pressRMsgs(m Model) (Model, []tea.Msg) {
	nm, cmd := m.Update(key("R"))
	m = nm.(Model)
	out := flatten(cmd)
	for _, r := range out {
		nm, _ = m.Update(r)
		m = nm.(Model)
	}
	return m, out
}

// pressR presses R and reports whether a RestartRequestMsg came out.
func pressR(m Model) (Model, bool) {
	m, out := pressRMsgs(m)
	return m, slices.Contains(out, tea.Msg(RestartRequestMsg{}))
}

func TestRestartDisabledInAttachMode(t *testing.T) {
	m := restartModel(t)
	m, asked := pressR(m)
	if asked {
		t.Fatal("R asked for a restart without --up")
	}
	if strings.Contains(footerLine(m), "R restart tilt") {
		t.Fatalf("footer = %q, R hint without --up", footerLine(m))
	}
}

func TestRestartDisabledWhileChildRuns(t *testing.T) {
	m := restartModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, asked := pressR(m)
	if asked {
		t.Fatal("R asked for a restart while tilt runs")
	}
	if strings.Contains(footerLine(m), "R restart tilt") {
		t.Fatalf("footer = %q, R hint while tilt runs", footerLine(m))
	}
}

func TestRestartAfterExit(t *testing.T) {
	m := restartModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: 1, Stderr: "Error: Tiltfile failed\n"})
	if f := footerLine(m); !strings.Contains(f, "r trigger · x stderr · R restart tilt · tab switch") {
		t.Fatalf("footer = %q, want the R hint after the x hint", f)
	}

	m, asked := pressR(m)
	if !asked {
		t.Fatal("R did not ask for a restart after the exit")
	}
	if f := footerLine(m); !strings.Contains(f, "restarting tilt…") {
		t.Fatalf("footer = %q, want the restart flash", f)
	}
	if _, again := pressR(m); again {
		t.Fatal("a second R asked again before main answered")
	}

	// main started tilt again: R is off until the next exit.
	m, _ = send(m, ChildStartedMsg{})
	if _, asked := pressR(m); asked {
		t.Fatal("R asked for a restart after the new start")
	}
	if s := screen(m); !strings.Contains(s, "● starting tilt…") {
		t.Fatalf("no starting state after the restart:\n%s", s)
	}

	// The new child failed too: R works again.
	m, _ = send(m, ChildExitedMsg{Code: -1, Stderr: "tilt up exited: port taken"})
	if _, asked := pressR(m); !asked {
		t.Fatal("R did not ask for a restart after a failed restart")
	}
}

func TestRestartFlashPrecedesRequest(t *testing.T) {
	m := restartModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: 1})
	// tea.Batch gives no order, so a fast exit could beat the flash.
	// tea.Sequence delivers the flash to Update before it runs the request.
	_, cmd := m.Update(key("R"))
	if name := reflect.TypeOf(cmd()).Name(); name != "sequenceMsg" {
		t.Fatalf("R command returned %s, want a tea.Sequence", name)
	}
	out := flatten(cmd)
	if len(out) != 2 {
		t.Fatalf("R produced %d messages, want 2: %v", len(out), out)
	}
	if _, ok := out[0].(flashMsg); !ok {
		t.Fatalf("first message = %T, want the flash", out[0])
	}
	if out[1] != tea.Msg(RestartRequestMsg{}) {
		t.Fatalf("second message = %T, want RestartRequestMsg", out[1])
	}
}

func TestRestartFailureShowsHintAtOnce(t *testing.T) {
	tests := []struct {
		name    string
		started bool // the failed child started before it exited
	}{
		{"start fails", false},
		{"child dies at once", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := restartModel(t)
			m, _ = send(m, ChildStartedMsg{})
			m, _ = send(m, ChildExitedMsg{Code: 1})
			m, asked := pressR(m)
			if !asked {
				t.Fatal("R did not ask for a restart")
			}
			if tt.started {
				m, _ = send(m, ChildStartedMsg{})
			}
			m, _ = send(m, ChildExitedMsg{Code: 1})
			if f := footerLine(m); !strings.Contains(f, "R restart tilt") {
				t.Fatalf("footer = %q, want the R hint right after a failed restart", f)
			}
		})
	}
}

func TestRestartClearKeepsLaterFlash(t *testing.T) {
	m := restartModel(t)
	clock := now
	m.d.Now = func() time.Time { return clock }
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: 1})
	m, _ = pressR(m)

	// Another feature flashes while the restart runs, but its message
	// reaches Update only after the child exited.
	clock = clock.Add(time.Second)
	other := flashMsg{text: "triggering api…", at: clock}
	clock = clock.Add(time.Second)
	m, _ = send(m, ChildExitedMsg{Code: 1})
	m, _ = send(m, other)
	if f := footerLine(m); !strings.Contains(f, "triggering api…") {
		t.Fatalf("footer = %q, want the flash stamped after the restart", f)
	}
}

func TestChildExitKeepsOtherFlash(t *testing.T) {
	m := restartModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, flashMsg{text: "triggering api…", at: m.d.Now()})
	m, _ = send(m, ChildExitedMsg{Code: 1})
	if f := footerLine(m); !strings.Contains(f, "triggering api…") {
		t.Fatalf("footer = %q, want the other flash kept", f)
	}
}

func TestRestartStoreResetsOnNewSnapshot(t *testing.T) {
	m := restartModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: 1})
	m, _ = pressR(m)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, snapshot(t, true))
	s := screen(m)
	if !strings.Contains(s, "● live") || !strings.Contains(s, "reconnected · logs reloaded") {
		t.Fatalf("the new snapshot did not reset the store:\n%s", s)
	}
	if strings.Count(s, "api starting") != 1 {
		t.Fatalf("logs from the old tilt survived the new snapshot:\n%s", s)
	}
}

func TestRestartStateIsPerModel(t *testing.T) {
	withFeatures(t, childFeature(), restartFeature())
	a, _ := setup(t, 140, 20, Deps{})
	b, _ := setup(t, 140, 20, Deps{})
	a, _ = send(a, ChildStartedMsg{})
	b, _ = send(b, ChildStartedMsg{})
	a, _ = send(a, ChildExitedMsg{Code: 1})
	if _, asked := pressR(b); asked {
		t.Fatal("R on model b used model a's exit")
	}
	if _, asked := pressR(a); !asked {
		t.Fatal("R on model a did not ask for a restart")
	}
}
