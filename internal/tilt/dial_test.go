package tilt

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeResolver map[string][]net.IP

func (f fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := f[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, len(ips))
	for i, ip := range ips {
		out[i] = net.IPAddr{IP: ip}
	}
	return out, nil
}

func withResolver(t *testing.T, r fakeResolver) {
	t.Helper()
	old := resolver
	resolver = r
	t.Cleanup(func() { resolver = old })
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckHost(t *testing.T) {
	withResolver(t, fakeResolver{
		"localhost":    {net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		"tilt.example": {net.ParseIP("203.0.113.7")},
		"mixed":        {net.ParseIP("127.0.0.1"), net.ParseIP("203.0.113.7")},
	})
	tests := []struct {
		host        string
		allowRemote bool
		wantRemote  bool
	}{
		{"localhost", false, false},
		{"127.0.0.1", false, false},
		{"tilt.example", false, true},
		{"mixed", false, true},
		{"tilt.example", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			c, err := New(tt.host, 10350, Options{AllowRemote: tt.allowRemote})
			if err != nil {
				t.Fatal(err)
			}
			err = c.CheckHost(context.Background())
			if got := errors.Is(err, ErrRemoteHost); got != tt.wantRemote {
				t.Fatalf("CheckHost(%s) = %v, want ErrRemoteHost=%v", tt.host, err, tt.wantRemote)
			}
			if tt.wantRemote && !strings.Contains(err.Error(), "ssh -L 10350:localhost:10350") {
				t.Errorf("error lacks ssh -L hint: %v", err)
			}
		})
	}
}

// IP literals still go through resolveAllowed. net.DefaultResolver returns
// them without DNS, so no fake is needed.
func TestDialRefusesRemoteIP(t *testing.T) {
	c, err := New("192.0.2.1", 10350, Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Trigger(context.Background(), "api")
	if !errors.Is(err, ErrRemoteHost) {
		t.Fatalf("Trigger to 192.0.2.1 = %v, want ErrRemoteHost", err)
	}
}

// The dialer must check every dial, not just the first. Re-pointing the
// name after one good request models DNS changing under a running client.
func TestDialChecksEveryConnection(t *testing.T) {
	_, c0 := newFake(t)
	port := c0.base.Port()
	r := fakeResolver{"tilt.test": {net.ParseIP("127.0.0.1")}}
	withResolver(t, r)
	c, err := New("tilt.test", mustAtoi(t, port), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ensureSession(context.Background()); err != nil {
		t.Fatalf("first dial: %v", err)
	}
	c.http.CloseIdleConnections()
	r["tilt.test"] = []net.IP{net.ParseIP("203.0.113.7")}
	_, err = c.do(context.Background(), http.MethodGet, "/api/websocket_token", nil)
	if !errors.Is(err, ErrRemoteHost) {
		t.Fatalf("second dial after DNS change = %v, want ErrRemoteHost", err)
	}
}

func TestNoProxyWithoutAllowRemote(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)

	c, err := New("192.0.2.1", 10350, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tr := c.http.Transport.(*http.Transport); tr.Proxy != nil {
		t.Fatal("transport has a Proxy func without AllowRemote")
	}
	if err := c.Trigger(context.Background(), "api"); !errors.Is(err, ErrRemoteHost) {
		t.Fatalf("Trigger = %v, want ErrRemoteHost", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("proxy received %d requests, want 0", n)
	}

	cr, err := New("192.0.2.1", 10350, Options{AllowRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	if tr := cr.http.Transport.(*http.Transport); tr.Proxy == nil {
		t.Fatal("AllowRemote transport ignores the proxy environment")
	}
}
