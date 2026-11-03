package resourcelist

import (
	"strings"
	"testing"

	"github.com/sentiolabs/go-tilt-tui/internal/logstore"
	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
)

// --- Contract assertions ---

var _ func(*Model, string) bool = (*Model).Select

// --- Behavior tests ---

func TestSelectExpandsGroup(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("api", labels("app", "be")), res("web", labels("app", "fe"))})
	m.HandleKey(key("c")) // collapse every group
	if !m.Select("web") {
		t.Fatal("Select(web) = false")
	}
	if m.Selected() != (logstore.Source{Resource: "web"}) || m.SelectedResource().Name() != "web" {
		t.Fatalf("Selected = %+v", m.Selected())
	}
	got := strings.Join(plain(m, 30, 5), "\n")
	if !strings.Contains(got, "▼ fe (1)") || !strings.Contains(got, "▶ be (1)") {
		t.Fatalf("Select did not expand only web's group:\n%s", got)
	}
}

func TestSelectUnknownChangesNothing(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("b")})
	m.HandleKey(key("j"))
	if m.Select("nope") {
		t.Fatal("Select(nope) = true")
	}
	if m.Selected() != (logstore.Source{Resource: "a"}) {
		t.Fatalf("Selected = %+v, want a", m.Selected())
	}
}

func TestSelectPinsHiddenResource(t *testing.T) {
	m := New(th(t))
	m.SetResources([]tilt.Resource{res("a"), res("off", disabled())})
	m.HandleKey(key("d")) // hide disabled resources
	if !m.Select("off") {
		t.Fatal("Select(off) = false")
	}
	if got := strings.Join(plain(m, 30, 4), "\n"); !strings.Contains(got, "off (filtered)") {
		t.Fatalf("selected hidden resource not listed:\n%s", got)
	}
	m.HandleKey(key("k"))
	if got := strings.Join(plain(m, 30, 4), "\n"); strings.Contains(got, "off") {
		t.Fatalf("hidden resource still listed after the cursor left:\n%s", got)
	}
}
