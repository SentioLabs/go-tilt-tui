package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// paletteSetup is setup with only the root commands and the palette.
func paletteSetup(t *testing.T, w, h int) (Model, *fakeClient) {
	t.Helper()
	withFeatures(t, paletteFeature)
	return setup(t, w, h, Deps{})
}

// paletteTop returns the top overlay as a palette, or fails.
func paletteTop(t *testing.T, m Model) *overlay.Palette {
	t.Helper()
	n := len(m.overlays)
	if n == 0 {
		t.Fatal("no overlay open")
	}
	p, ok := m.overlays[n-1].(*overlay.Palette)
	if !ok {
		t.Fatalf("top overlay is %T", m.overlays[n-1])
	}
	return p
}

var paletteGap = regexp.MustCompile(`\s{2,}`)

// paletteListed is the command titles the open palette lists.
func paletteListed(t *testing.T, m Model) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(paletteTop(t, m).View(80, 100)), "\n")
	var out []string
	for _, l := range lines[1:] {
		out = append(out, paletteGap.Split(strings.TrimSpace(l), 2)[0])
	}
	return out
}

// paletteSendAll delivers msg and every message that follows from it,
// however deep, except quit. Running a command from the palette takes
// two steps: the palette's PaletteRunMsg, then the command's own
// messages.
func paletteSendAll(m Model, msg tea.Msg) Model {
	nm, cmd := m.Update(msg)
	m = nm.(Model)
	for _, r := range drain(cmd) {
		if _, quit := r.(tea.QuitMsg); quit {
			continue
		}
		m = paletteSendAll(m, r)
	}
	return m
}

func paletteRun(m Model, query string) Model {
	m = press(m, ":")
	for _, r := range query {
		m = press(m, string(r))
	}
	return paletteSendAll(m, key("enter"))
}

func TestPaletteListsOnlyEnabledCommands(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = press(m, ":")
	got := paletteListed(t, m)
	for _, title := range []string{"trigger", "scroll", "follow on/off", "command palette"} {
		if slices.Contains(got, title) {
			t.Errorf("palette lists %q on All with list focus: %q", title, got)
		}
	}
	if !slices.Contains(got, "help") || !slices.Contains(got, "quit") {
		t.Fatalf("palette lacks global commands: %q", got)
	}

	m, _ = paletteSetup(t, 140, 30)
	m = press(m, "j", "j", ":")
	if got := paletteListed(t, m); !slices.Contains(got, "trigger") {
		t.Fatalf("palette lacks trigger on api: %q", got)
	}
}

func TestPaletteRunsCommand(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = paletteRun(m, "help")
	if len(m.overlays) != 1 {
		t.Fatalf("%d overlays open, want only help", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*overlay.Palette); ok {
		t.Fatal("the palette is still open")
	}
	if !strings.Contains(screen(m), "Help") {
		t.Fatalf("help did not open:\n%s", screen(m))
	}
}

func TestPaletteRunsWithCurrentContext(t *testing.T) {
	m, fc := paletteSetup(t, 140, 30)
	m = press(m, "j", "j")
	m = paletteRun(m, "trigger")
	if !slices.Equal(fc.triggered, []string{"api"}) {
		t.Fatalf("triggered = %q", fc.triggered)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays open after trigger", len(m.overlays))
	}
}

func TestPaletteKeysStayInTopOverlay(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	before := m.list.Selected()
	m = press(m, ":", "j")
	if m.list.Selected() != before {
		t.Fatal("j reached the list while the palette was open")
	}
	if first := strings.Split(ansi.Strip(paletteTop(t, m).View(80, 5)), "\n")[0]; !strings.Contains(first, "j") {
		t.Fatalf("j did not reach the query: %q", first)
	}
}

func TestPaletteEscAndCtrlC(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = press(m, ":")
	_, msgs := send(m, key("ctrl+c"))
	if !slices.ContainsFunc(msgs, func(x tea.Msg) bool { _, ok := x.(tea.QuitMsg); return ok }) {
		t.Fatal("ctrl+c did not quit with the palette open")
	}
	m = press(m, "esc")
	if len(m.overlays) != 0 {
		t.Fatal("esc did not close the palette")
	}
}

func TestPalettePasteEditsQuery(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = press(m, ":")
	m, _ = send(m, tea.PasteMsg{Content: "hel"})
	if first := strings.Split(ansi.Strip(paletteTop(t, m).View(80, 5)), "\n")[0]; !strings.Contains(first, "hel") {
		t.Fatalf("paste did not reach the query: %q", first)
	}
}

func TestPaletteStaleID(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = press(m, ":")
	m, _ = send(m, overlay.PaletteRunMsg{ID: "nope"})
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays open, want the palette popped", len(m.overlays))
	}
	if s := screen(m); !strings.Contains(s, "command not available") {
		t.Fatalf("no flash:\n%s", s)
	}
}

func TestPaletteGolden(t *testing.T) {
	m, _ := paletteSetup(t, 140, 30)
	m = press(m, ":")
	checkFrame(t, m, 140, 30)
	got := screen(m)
	path := filepath.Join("testdata", "palette_140.golden")
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
}
