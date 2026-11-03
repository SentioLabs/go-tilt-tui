package tilt

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// dumpClient serves write as /api/dump/engine behind Tilt's cookie check
// and returns a client for it.
func dumpClient(t *testing.T, write func(w io.Writer)) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: fakeToken, Path: "/"})
	})
	mux.HandleFunc("GET /api/dump/engine", func(w http.ResponseWriter, r *http.Request) {
		if ck, err := r.Cookie(sessionCookie); err != nil || ck.Value != fakeToken {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		write(w)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	c, err := New(host, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const dumpSmall = `{"ManifestTargets":{
"api":{"Manifest":{"Name":"api","ResourceDependencies":["db"]},"State":{}},
"db":{"Manifest":{"Name":"db","ResourceDependencies":null},"State":{}},
"web":{"Manifest":{"Name":"web","ResourceDependencies":["api","db"]},"State":{}}},
"Token":"secret","TiltStartTime":"2026-11-03T12:00:00Z"}`

func TestResourceDependencies(t *testing.T) {
	c := dumpClient(t, func(w io.Writer) { io.WriteString(w, dumpSmall) })
	got, err := c.ResourceDependencies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"api": {"db"}, "db": nil, "web": {"api", "db"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A dump past the 1 MiB default body limit still decodes (SC 14).
func TestResourceDependenciesLargeDump(t *testing.T) {
	pad := strings.Repeat("x", 3<<20)
	c := dumpClient(t, func(w io.Writer) {
		io.WriteString(w, `{"LogStore":"`+pad+`",`+strings.TrimPrefix(dumpSmall, "{"))
	})
	got, err := c.ResourceDependencies(context.Background())
	if err != nil || len(got["web"]) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestResourceDependenciesTooLarge(t *testing.T) {
	c := dumpClient(t, func(w io.Writer) {
		io.WriteString(w, `{"LogStore":"`)
		chunk := []byte(strings.Repeat("x", 1<<20))
		for range 65 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		io.WriteString(w, `"}`)
	})
	if _, err := c.ResourceDependencies(context.Background()); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestResourceDependenciesUnknownFormat(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, `{"ManifestTargets":[1,2]}`, `[]`} {
		c := dumpClient(t, func(w io.Writer) { io.WriteString(w, body) })
		if _, err := c.ResourceDependencies(context.Background()); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("%s: err = %v, want ErrUnknownFormat", body, err)
		}
	}
}

// The error never carries dump content such as Tilt's token.
func TestResourceDependenciesErrorHidesDump(t *testing.T) {
	c := dumpClient(t, func(w io.Writer) { io.WriteString(w, `{"Token":"secret-token","ManifestTargets":7}`) })
	_, err := c.ResourceDependencies(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err = %v", err)
	}
}
