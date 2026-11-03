package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sentiolabs/go-tilt-tui/internal/tilt"
	"github.com/sentiolabs/go-tilt-tui/internal/ui/overlay"
)

// openLinks is a ViewMsg that gives api these endpoint links.
func openLinks(t *testing.T, links ...tilt.EndpointLink) ViewMsg {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"uiResources": []map[string]any{{
		"metadata": map[string]any{"name": "api", "labels": map[string]string{"app": "backend"}},
		"status":   map[string]any{"order": 2, "updateStatus": "error", "endpointLinks": links},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var v tilt.View
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return ViewMsg(v)
}

// openRecorder records Deps.Open calls and fails them with err.
type openRecorder struct {
	urls []string
	err  error
}

func (o *openRecorder) open(_ context.Context, url string) error {
	o.urls = append(o.urls, url)
	return o.err
}

// openSetup is setup with only the open feature, api given links and the
// cursor on api.
func openSetup(t *testing.T, w int, links ...tilt.EndpointLink) (Model, *openRecorder) {
	t.Helper()
	withFeatures(t, openFeature)
	rec := &openRecorder{}
	m, _ := setup(t, w, 20, Deps{Open: rec.open})
	m, _ = send(m, openLinks(t, links...))
	m = press(m, "j", "j") // All, ▼ backend, api
	if r := m.list.SelectedResource(); r == nil || r.Name() != "api" {
		t.Fatalf("cursor not on api: %v", r)
	}
	return m, rec
}

func TestOpenSingleLinkOpensAtOnce(t *testing.T) {
	m, rec := openSetup(t, 140, tilt.EndpointLink{URL: "http://localhost:8080", Name: "web"})
	m = press(m, "o")
	if len(m.overlays) != 0 {
		t.Fatal("one link opened a menu")
	}
	if !slices.Equal(rec.urls, []string{"http://localhost:8080"}) {
		t.Fatalf("Open got %q", rec.urls)
	}
	if s := screen(m); !strings.Contains(s, "opened http://localhost:8080") {
		t.Fatalf("no result flash:\n%s", s)
	}
}

func TestOpenSeveralLinksShowsMenu(t *testing.T) {
	m, rec := openSetup(t, 140,
		tilt.EndpointLink{URL: "http://localhost:8080", Name: "web"},
		tilt.EndpointLink{URL: "http://localhost:9090/metrics"},
	)
	m = press(m, "o")
	if len(m.overlays) != 1 {
		t.Fatal("several links did not open the menu")
	}
	if _, ok := m.overlays[0].(*overlay.Links); !ok {
		t.Fatalf("top overlay is %T", m.overlays[0])
	}
	if len(rec.urls) != 0 {
		t.Fatal("the menu opened a link before a choice")
	}
	m = press(m, "2")
	if len(m.overlays) != 0 {
		t.Fatal("choosing a link did not close the menu")
	}
	if !slices.Equal(rec.urls, []string{"http://localhost:9090/metrics"}) {
		t.Fatalf("Open got %q", rec.urls)
	}
}

func TestOpenErrorFlashes(t *testing.T) {
	m, rec := openSetup(t, 140, tilt.EndpointLink{URL: "file:///etc/passwd"})
	rec.err = errors.New(`refusing scheme "file"`)
	m = press(m, "o")
	if s := screen(m); !strings.Contains(s, `open failed: refusing scheme "file"`) {
		t.Fatalf("no error flash:\n%s", s)
	}
}

func TestOpenDisabledWithoutLinks(t *testing.T) {
	m, rec := openSetup(t, 140)
	m = press(m, "o")
	m = press(m, "g", "o") // All Resources
	if len(rec.urls) != 0 || len(m.overlays) != 0 {
		t.Fatalf("o did something without links: %q", rec.urls)
	}
	m = press(m, "?")
	if strings.Contains(screen(m), "open endpoint link") {
		t.Fatalf("help lists a disabled command:\n%s", screen(m))
	}
}

func TestOpenHintWhenEnabled(t *testing.T) {
	m, _ := openSetup(t, 140, tilt.EndpointLink{URL: "http://localhost:8080"})
	if s := screen(m); !strings.Contains(s, "r trigger · o open · tab switch") {
		t.Fatalf("footer lacks the open hint:\n%s", s)
	}
}

func TestOpenTextPassesThroughTermsafe(t *testing.T) {
	evil := "http://x\x1b]52;c;ZXZpbA==\x07y"
	m, rec := openSetup(t, 140, tilt.EndpointLink{URL: evil, Name: "n\x1b]0;t\x07"}, tilt.EndpointLink{URL: "http://ok"})
	m = press(m, "o")
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07\u009b") {
		t.Fatalf("non-SGR control in the menu: %q", rest)
	}
	m = press(m, "1")
	if rest := sgr.ReplaceAllString(m.render(), ""); strings.ContainsAny(rest, "\x1b\x07\u009b") {
		t.Fatalf("non-SGR control in the flash: %q", rest)
	}
	if !slices.Equal(rec.urls, []string{evil}) {
		t.Fatalf("Open got %q, want the raw URL", rec.urls)
	}
}

func TestOpenGolden(t *testing.T) {
	m, _ := openSetup(t, 140,
		tilt.EndpointLink{URL: "http://localhost:8080", Name: "web"},
		tilt.EndpointLink{URL: "http://localhost:9090/metrics"},
	)
	m = press(m, "o")
	checkFrame(t, m, 140, 20)
	got := screen(m)
	path := filepath.Join("testdata", "open_links_140.golden")
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
