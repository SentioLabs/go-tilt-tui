package overlay

import (
	"maps"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

func listButtons() []tilt.Button {
	stop := tilt.Button{
		Metadata: tilt.ObjectMeta{Name: "api-stopbuild", ResourceVersion: "3"},
		Spec:     tilt.ButtonSpec{Text: "Stop Build", Disabled: true},
	}
	seed := tilt.Button{
		Metadata: tilt.ObjectMeta{Name: "api-seed", ResourceVersion: "4"},
		Spec:     tilt.ButtonSpec{Text: "Seed DB", Inputs: []tilt.InputSpec{formHidden}},
	}
	return []tilt.Button{seed, formButton(formText), stop}
}

func TestButtonsListView(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	if o.Title() != "Buttons: api" {
		t.Fatalf("title = %q", o.Title())
	}
	got := ansi.Strip(o.View(60, 10))
	want := strings.Join([]string{
		"› Seed DB",
		"  Deploy",
		"  Stop Build (disabled)",
		"",
		"enter click · esc close",
	}, "\n")
	if got != want {
		t.Fatalf("view =\n%s\nwant\n%s", got, want)
	}
	raw := o.View(60, 10)
	if !strings.Contains(raw, formTheme(t).Dim.Render("  Stop Build (disabled)")) {
		t.Fatalf("disabled button not dimmed: %q", raw)
	}
}

func TestButtonsEnterClicksPlainButton(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	o2, msgs := formPress(o, "enter")
	got, closed := formClicks(msgs)
	if o2 != o || !closed || len(got) != 1 {
		t.Fatalf("enter on Seed DB: clicks %v closed %v", got, closed)
	}
	if got[0].Button.Metadata.Name != "api-seed" || !maps.Equal(got[0].Values, map[string]any{"token": "s3cret"}) {
		t.Fatalf("click = %#v", got[0])
	}
}

func TestButtonsEnterOpensForm(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	o, msgs := formPress(o, "j", "enter")
	if len(msgs) != 0 || o.Title() != "Deploy" {
		t.Fatalf("enter on Deploy: title %q msgs %v", o.Title(), msgs)
	}
	if v := ansi.Strip(o.View(60, 10)); !strings.Contains(v, "› Message: v1") {
		t.Fatalf("form view:\n%s", v)
	}
}

func TestButtonsDisabledCannotBeClicked(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	o, _ = formPress(o, "j", "j", "j") // the cursor stops on the last row
	o2, msgs := formPress(o, "enter")
	if o2 != o || len(msgs) != 0 {
		t.Fatalf("enter on a disabled button: overlay %v msgs %v", o2.Title(), msgs)
	}
	if v := ansi.Strip(o.View(60, 10)); !strings.Contains(v, "› Stop Build (disabled)") {
		t.Fatalf("cursor not on Stop Build:\n%s", v)
	}
}

func TestButtonsNavigationAndClose(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	o, _ = formPress(o, "down", "up", "k")
	if v := ansi.Strip(o.View(60, 10)); !strings.HasPrefix(v, "› Seed DB") {
		t.Fatalf("cursor left the first row:\n%s", v)
	}
	for _, k := range []string{"esc", "q"} {
		_, msgs := formPress(o, k)
		if got, closed := formClicks(msgs); len(got) != 0 || !closed {
			t.Fatalf("%s: clicks %v closed %v", k, got, closed)
		}
	}
	if v := ansi.Strip(NewButtons(formTheme(t), "api", nil).View(60, 10)); v != "no buttons" {
		t.Fatalf("empty list view = %q", v)
	}
}

func TestButtonsViewFitsSize(t *testing.T) {
	o := NewButtons(formTheme(t), "api", listButtons())
	o, _ = formPress(o, "j", "j")
	lines := strings.Split(o.View(8, 2), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2", len(lines))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 8 {
			t.Fatalf("line %q is %d wide", l, w)
		}
	}
	if !strings.Contains(ansi.Strip(lines[1]), "›") {
		t.Fatalf("cursor row scrolled out: %q", lines)
	}
}
