package proc

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The host is always localhost, whatever the environment says.
func TestFileWatchesArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	bin := fakeTilt(t, `printf '%s\n' "$@" > "`+argsFile+`"
echo '{"kind":"List","items":[]}'`)
	t.Setenv("TILT_HOST", "evil.example")
	out, err := (Tilt{Binary: bin}).FileWatches(context.Background(), 10399)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != `{"kind":"List","items":[]}` {
		t.Fatalf("out = %q", out)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"get", "filewatches", "-o", "json", "--host", "localhost", "--port", "10399"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q, want %q", got, want)
	}
}

func TestFileWatchesNeedsBinary(t *testing.T) {
	if _, err := (Tilt{}).FileWatches(context.Background(), testPort); err == nil {
		t.Fatal("want an error without a binary")
	}
}

func TestFileWatchesFailureCarriesStderr(t *testing.T) {
	bin := fakeTilt(t, `echo "Error: connection refused" >&2; exit 1`)
	_, err := (Tilt{Binary: bin}).FileWatches(context.Background(), testPort)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestFileWatchesOutputCap(t *testing.T) {
	bin := fakeTilt(t, `head -c 17000000 /dev/zero`)
	_, err := (Tilt{Binary: bin}).FileWatches(context.Background(), testPort)
	if err == nil || !strings.Contains(err.Error(), "output over") {
		t.Fatalf("err = %v", err)
	}
}

func TestFileWatchesHonoursContext(t *testing.T) {
	bin := fakeTilt(t, `exec sleep 60`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if _, err := (Tilt{Binary: bin}).FileWatches(ctx, testPort); err == nil {
		t.Fatal("want an error after the deadline")
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
