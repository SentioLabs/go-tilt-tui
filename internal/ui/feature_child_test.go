package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// childModel is a sized model with only the child feature registered.
// It has seen no snapshot yet.
func childModel(t *testing.T) Model {
	t.Helper()
	withFeatures(t, childFeature())
	m, _ := newModel(t, Deps{})
	m, _ = send(m, tea.WindowSizeMsg{Width: 140, Height: 20})
	return m
}

func footerLine(m Model) string {
	lines := strings.Split(screen(m), "\n")
	return lines[len(lines)-1]
}

func TestChildAttachModeShowsNothing(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ConnMsg{Err: errors.New("dial tcp: connection refused")})
	s := screen(m)
	if !strings.Contains(s, "● disconnected: dial tcp") {
		t.Fatalf("attach mode lost the connection error:\n%s", s)
	}
	if strings.Contains(s, "starting tilt") || strings.Contains(s, "tilt exited") {
		t.Fatalf("child state shown without --up:\n%s", s)
	}
	m = press(m, "x")
	if strings.Contains(footerLine(m), "x stderr") {
		t.Fatalf("x hint shown without an exited child: %q", footerLine(m))
	}
}

func TestChildStartingHidesConnectionErrors(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ConnMsg{Connecting: true})
	if s := screen(m); !strings.Contains(s, "● starting tilt…  localhost:10350") {
		t.Fatalf("no starting state while connecting:\n%s", s)
	}
	m, _ = send(m, ConnMsg{Err: errors.New("dial tcp: connection refused")})
	s := screen(m)
	if !strings.Contains(s, "● starting tilt…") || strings.Contains(s, "disconnected") {
		t.Fatalf("a connection error replaced the starting state:\n%s", s)
	}
	m, _ = send(m, snapshot(t, false))
	if s := screen(m); !strings.Contains(s, "● starting tilt…") {
		t.Fatalf("an incomplete view ended the starting state:\n%s", s)
	}
	m, _ = send(m, snapshot(t, true))
	s = screen(m)
	if !strings.Contains(s, "● live") || strings.Contains(s, "starting tilt") {
		t.Fatalf("the first snapshot did not end the starting state:\n%s", s)
	}
}

func TestChildExitBeforeFirstSnapshot(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: 1, Stderr: "Error: Tiltfile not found\n"})
	s := screen(m)
	if !strings.Contains(s, "● tilt exited (code 1)") || strings.Contains(s, "starting tilt") {
		t.Fatalf("exit did not replace the starting state:\n%s", s)
	}
	if !strings.Contains(s, "Error: Tiltfile not found") {
		t.Fatalf("panel lacks the stderr tail:\n%s", s)
	}
}

func TestChildExitShowsCodeTailAndKeepsLogs(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, snapshot(t, true))
	var stderr strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&stderr, "line %03d\n", i)
	}
	stderr.WriteString("Error: bad\x1b]52;c;ZXZpbA==\x07 Tiltfile\n\n")
	m, _ = send(m, ChildExitedMsg{Code: 2, Stderr: stderr.String()})
	// The watch loop has stopped, but a late drop must not hide the exit.
	m, _ = send(m, ConnMsg{Err: errors.New("websocket: closed")})

	s := screen(m)
	for _, want := range []string{"● tilt exited (code 2)", "tilt stderr · x shows logs", "Error: bad Tiltfile", "line 100", "line 085"} {
		if !strings.Contains(s, want) {
			t.Errorf("exited screen lacks %q:\n%s", want, s)
		}
	}
	// 20 rows less the footer and two borders leave 17 lines: the error
	// and lines 085 to 100.
	if strings.Contains(s, "line 084") || strings.Contains(s, "disconnected") {
		t.Errorf("panel shows more than fits, or the drop replaced the exit:\n%s", s)
	}
	if strings.Contains(s, "api starting") {
		t.Errorf("log lines drawn under the panel:\n%s", s)
	}
	if !strings.Contains(footerLine(m), "r trigger · x stderr · tab switch") {
		t.Errorf("footer = %q, want the x hint", footerLine(m))
	}
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("stderr reached the terminal unfiltered: %q", rest)
	}

	m = press(m, "x")
	s = screen(m)
	if !strings.Contains(s, "api starting") || strings.Contains(s, "line 100") {
		t.Fatalf("x did not bring the logs back:\n%s", s)
	}
	if !strings.Contains(s, "● tilt exited (code 2)") {
		t.Fatalf("the exit left the status line with the panel:\n%s", s)
	}
	m = press(m, "x")
	if s := screen(m); !strings.Contains(s, "line 100") {
		t.Fatalf("x did not bring the panel back:\n%s", s)
	}
	checkFrame(t, m, 140, 20)
}

func TestChildExitWithoutCodeOrStderr(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, ChildExitedMsg{Code: -1})
	s := screen(m)
	if !strings.Contains(s, "● tilt exited  localhost:10350") || !strings.Contains(s, "tilt wrote nothing to stderr") {
		t.Fatalf("signal exit screen:\n%s", s)
	}
}

func TestChildStartAfterExitClearsPanel(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, snapshot(t, true))
	m, _ = send(m, ChildExitedMsg{Code: 1, Stderr: "boom\n"})
	m, _ = send(m, ChildStartedMsg{})
	s := screen(m)
	if !strings.Contains(s, "● starting tilt…") || strings.Contains(s, "boom") || strings.Contains(s, "tilt exited") {
		t.Fatalf("a new start kept the old exit:\n%s", s)
	}
	if strings.Contains(footerLine(m), "x stderr") {
		t.Fatalf("x hint left after a new start: %q", footerLine(m))
	}
}

func TestChildExitedGolden(t *testing.T) {
	m := childModel(t)
	m, _ = send(m, ChildStartedMsg{})
	m, _ = send(m, snapshot(t, true))
	m, _ = send(m, ChildExitedMsg{Code: 1, Stderr: "Loading Tiltfile at: /src/Tiltfile\nError: Tiltfile:3: undefined: local_resorce\n"})
	got := screen(m)
	path := filepath.Join("testdata", "child_exited_140.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("screen differs from %s:\n%s", path, got)
	}
}

func TestChildStateIsPerModel(t *testing.T) {
	withFeatures(t, childFeature())
	a, _ := newModel(t, Deps{})
	b, _ := newModel(t, Deps{})
	a, _ = send(a, tea.WindowSizeMsg{Width: 140, Height: 20})
	b, _ = send(b, tea.WindowSizeMsg{Width: 140, Height: 20})
	a, _ = send(a, ChildStartedMsg{})
	b, _ = send(b, ChildStartedMsg{})
	a, _ = send(a, ChildExitedMsg{Code: 1, Stderr: "only a\n"})

	if s := screen(a); !strings.Contains(s, "● tilt exited (code 1)") || !strings.Contains(s, "only a") {
		t.Fatalf("model a lost its exit:\n%s", s)
	}
	s := screen(b)
	if !strings.Contains(s, "● starting tilt…") || strings.Contains(s, "tilt exited") || strings.Contains(s, "only a") {
		t.Fatalf("model b shows model a's child state:\n%s", s)
	}
}
