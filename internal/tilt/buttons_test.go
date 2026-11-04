package tilt

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// clickRequest is one PUT the button fake received.
type clickRequest struct {
	method      string
	path        string // escaped, as sent on the wire
	name        string // decoded {name} path value
	contentType string
	raw         map[string]any
	body        clickPayload
}

// buttonFake answers the UIButton status endpoint behind the same session
// check as fakeTilt. Every other path goes to fakeTilt's handler.
type buttonFake struct {
	clicks chan clickRequest
	code   int    // response status, 0 means 200
	reply  string // response body, "" echoes the request with resourceVersion+1
}

func newButtonFake(t *testing.T) (*buttonFake, *Client) {
	t.Helper()
	bf := &buttonFake{clicks: make(chan clickRequest, 4)}
	base := &fakeTilt{t: t, triggers: make(chan map[string]any, 1)}
	mux := http.NewServeMux()
	mux.Handle("/", base.handler())
	mux.HandleFunc("PUT /proxy/apis/tilt.dev/v1alpha1/uibuttons/{name}/status", func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(sessionCookie)
		if err != nil || ck.Value != fakeToken {
			http.Error(w, "missing session", http.StatusUnauthorized)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req := clickRequest{
			method:      r.Method,
			path:        r.URL.EscapedPath(),
			name:        r.PathValue("name"),
			contentType: r.Header.Get("Content-Type"),
		}
		if err := json.Unmarshal(data, &req.raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(data, &req.body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bf.clicks <- req
		if bf.code != 0 {
			w.WriteHeader(bf.code)
		}
		if bf.reply != "" {
			_, _ = io.WriteString(w, bf.reply)
			return
		}
		rv, _ := strconv.Atoi(req.body.Metadata.ResourceVersion)
		out := Button{Metadata: req.body.Metadata, Status: req.body.Status}
		out.Metadata.ResourceVersion = strconv.Itoa(rv + 1)
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	c, err := New(host, port, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return bf, c
}

func withNow(t *testing.T, ts time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = old })
}

// formButton has one input of every kind, with defaults.
func formButton() Button {
	return Button{
		Metadata: ObjectMeta{
			Name:            "api-deploy",
			ResourceVersion: "41",
			Annotations:     map[string]string{"owner": "test"},
		},
		Spec: ButtonSpec{
			Location: &ButtonLocation{ComponentID: "api", ComponentType: ComponentTypeResource},
			Text:     "Deploy",
			Inputs: []InputSpec{
				{Name: "msg", Label: "Message", Text: &TextInputSpec{DefaultValue: "hello"}},
				{Name: "force", Label: "Force", Bool: &BoolInputSpec{DefaultValue: true}},
				{Name: "env", Label: "Env", Choice: &ChoiceInputSpec{Choices: []string{"dev", "prod"}}},
				{Name: "token", Hidden: &HiddenInputSpec{Value: "abc"}},
			},
		},
		Status: ButtonStatus{LastClickedAt: "2020-01-01T00:00:00.000000Z"},
	}
}

func inputMap(in []InputStatus) map[string]any {
	out := map[string]any{}
	for _, s := range in {
		switch {
		case s.Text != nil:
			out[s.Name] = "text:" + s.Text.Value
		case s.Bool != nil:
			out[s.Name] = s.Bool.Value
		case s.Choice != nil:
			out[s.Name] = "choice:" + s.Choice.Value
		case s.Hidden != nil:
			out[s.Name] = "hidden:" + s.Hidden.Value
		}
	}
	return out
}

var microTime = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$`)

func TestClickButtonSendsStatusPut(t *testing.T) {
	bf, c := newButtonFake(t)
	withNow(t, time.Date(2026, 11, 3, 10, 30, 0, 123456789, time.FixedZone("x", 3600)))

	got, err := c.ClickButton(context.Background(), formButton(), map[string]any{
		"msg":   "ship it",
		"force": false,
		"env":   "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := <-bf.clicks
	if req.path != "/proxy/apis/tilt.dev/v1alpha1/uibuttons/api-deploy/status" {
		t.Errorf("path = %q", req.path)
	}
	if req.contentType != "application/json" {
		t.Errorf("Content-Type = %q", req.contentType)
	}
	if _, ok := req.raw["spec"]; ok {
		t.Error("body carries spec, want metadata and status only")
	}
	if req.body.Metadata.Name != "api-deploy" || req.body.Metadata.ResourceVersion != "41" {
		t.Errorf("metadata = %+v", req.body.Metadata)
	}
	if req.body.Metadata.Annotations["owner"] != "test" {
		t.Errorf("annotations dropped: %+v", req.body.Metadata.Annotations)
	}
	// 10:30:00.123456789 at +01:00 is 09:30:00.123456 UTC, cut to micros.
	if at := req.body.Status.LastClickedAt; at != "2026-11-03T09:30:00.123456Z" {
		t.Errorf("lastClickedAt = %q", at)
	}
	want := map[string]any{
		"msg":   "text:ship it",
		"force": false,
		"env":   "choice:prod",
		"token": "hidden:abc",
	}
	gotIn := inputMap(req.body.Status.Inputs)
	for k, v := range want {
		if gotIn[k] != v {
			t.Errorf("input %s = %v, want %v", k, gotIn[k], v)
		}
	}
	if names := []string{req.body.Status.Inputs[0].Name, req.body.Status.Inputs[3].Name}; names[0] != "msg" || names[1] != "token" {
		t.Errorf("inputs out of spec order: %v", names)
	}
	if got.Metadata.ResourceVersion != "42" {
		t.Errorf("returned resourceVersion = %q, want the server's 42", got.Metadata.ResourceVersion)
	}
	if gotIn := inputMap(got.Status.Inputs); gotIn["msg"] != "text:ship it" {
		t.Errorf("returned button is not the decoded response: %+v", got.Status)
	}
}

func TestClickButtonDefaults(t *testing.T) {
	bf, c := newButtonFake(t)
	if _, err := c.ClickButton(context.Background(), formButton(), nil); err != nil {
		t.Fatal(err)
	}
	req := <-bf.clicks
	if at := req.body.Status.LastClickedAt; !microTime.MatchString(at) {
		t.Errorf("lastClickedAt = %q, want RFC 3339 with six fractional digits", at)
	}
	want := map[string]any{
		"msg":   "text:hello",
		"force": true,
		"env":   "choice:dev",
		"token": "hidden:abc",
	}
	gotIn := inputMap(req.body.Status.Inputs)
	for k, v := range want {
		if gotIn[k] != v {
			t.Errorf("input %s = %v, want %v", k, gotIn[k], v)
		}
	}
}

// The disable toggle from the recorded fixture: no values, so the hidden
// action Tilt last wrote is the one sent.
func TestClickButtonDisableToggleFromFixture(t *testing.T) {
	s := NewStore()
	for _, v := range loadFixture(t) {
		s.Apply(v)
	}
	toggle, ok := s.DisableButton("slow-build")
	if !ok {
		t.Fatal("fixture has no disable toggle for slow-build")
	}
	bf, c := newButtonFake(t)
	if _, err := c.ClickButton(context.Background(), toggle, nil); err != nil {
		t.Fatal(err)
	}
	req := <-bf.clicks
	if req.name != toggle.Metadata.Name || req.body.Metadata.ResourceVersion != toggle.Metadata.ResourceVersion {
		t.Errorf("sent %s@%s, want %s@%s", req.name, req.body.Metadata.ResourceVersion,
			toggle.Metadata.Name, toggle.Metadata.ResourceVersion)
	}
	want := "hidden:" + toggle.Spec.Inputs[0].Hidden.Value
	if got := inputMap(req.body.Status.Inputs)["action"]; got != want {
		t.Errorf("action = %v, want %v", got, want)
	}
}

func TestClickButtonEscapesName(t *testing.T) {
	bf, c := newButtonFake(t)
	b := Button{Metadata: ObjectMeta{Name: "(Tiltfile)-stopbuild", ResourceVersion: "6"}}
	if _, err := c.ClickButton(context.Background(), b, nil); err != nil {
		t.Fatal(err)
	}
	if req := <-bf.clicks; req.name != "(Tiltfile)-stopbuild" {
		t.Errorf("server saw name %q", req.name)
	}
}

func TestClickButtonConflict(t *testing.T) {
	bf, c := newButtonFake(t)
	bf.code = http.StatusConflict
	bf.reply = `{"kind":"Status","reason":"Conflict"}`
	_, err := c.ClickButton(context.Background(), formButton(), nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ClickButton on 409 = %v, want ErrConflict", err)
	}
	if len(bf.clicks) != 1 {
		t.Errorf("server saw %d clicks, want exactly 1 (no retry)", len(bf.clicks))
	}
}

func TestClickButtonServerError(t *testing.T) {
	bf, c := newButtonFake(t)
	bf.code = http.StatusInternalServerError
	bf.reply = "boom"
	_, err := c.ClickButton(context.Background(), formButton(), nil)
	if err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("ClickButton on 500 = %v, want a non-conflict error", err)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error lacks status and body: %v", err)
	}
}

func TestClickButtonBadResponse(t *testing.T) {
	bf, c := newButtonFake(t)
	bf.reply = "not json"
	if _, err := c.ClickButton(context.Background(), formButton(), nil); err == nil {
		t.Fatal("ClickButton decoded a non-JSON response")
	}
}

func TestClickButtonOversizedResponse(t *testing.T) {
	bf, c := newButtonFake(t)
	bf.reply = `"` + strings.Repeat("x", defaultMaxBody) + `"`
	if _, err := c.ClickButton(context.Background(), formButton(), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ClickButton = %v, want ErrTooLarge", err)
	}
}

func TestClickButtonRefusesBadInput(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
	}{
		{"unknown input", map[string]any{"nope": "x"}},
		{"text takes string", map[string]any{"msg": 3}},
		{"bool takes bool", map[string]any{"force": "true"}},
		{"choice takes string", map[string]any{"env": true}},
		{"choice off the list", map[string]any{"env": "staging"}},
		{"hidden takes string", map[string]any{"token": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bf, c := newButtonFake(t)
			if _, err := c.ClickButton(context.Background(), formButton(), tt.values); err == nil {
				t.Fatal("ClickButton accepted bad input")
			}
			if len(bf.clicks) != 0 {
				t.Error("bad input still reached the server")
			}
		})
	}
}

func TestClickButtonRefusesBadName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../../api/trigger"} {
		bf, c := newButtonFake(t)
		_, err := c.ClickButton(context.Background(), Button{Metadata: ObjectMeta{Name: name}}, nil)
		if err == nil {
			t.Errorf("ClickButton(%q) succeeded", name)
		}
		if len(bf.clicks) != 0 {
			t.Errorf("ClickButton(%q) reached the server", name)
		}
	}
}

// ClickButton goes through the guarded transport like every other call.
func TestClickButtonRefusesRemoteHost(t *testing.T) {
	c, err := New("192.0.2.1", 10350, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClickButton(context.Background(), formButton(), nil); !errors.Is(err, ErrRemoteHost) {
		t.Fatalf("ClickButton to 192.0.2.1 = %v, want ErrRemoteHost", err)
	}
}
