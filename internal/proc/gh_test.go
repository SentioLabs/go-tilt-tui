package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// --- Contract assertions ---

var _ func(context.Context, string, string, string) error = VerifyAttestation

// --- Behavior tests ---

const (
	testRepo     = "SentioLabs/go-tilt-tui"
	testWorkflow = "SentioLabs/go-tilt-tui/.github/workflows/release.yml"
)

// fakeGH writes an executable sh script called gh into a new temp dir,
// sets PATH to that dir alone and returns the dir. The script resets its
// own PATH, so it can still run sleep, head and tr.
func fakeGH(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestVerifyAttestationPasses(t *testing.T) {
	for _, path := range []string{"/tmp/x/checksums.txt", "/tmp/x y/checksums.txt"} {
		t.Run(path, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args")
			fakeGH(t, `printf '%s\n' "$@" > "`+argsFile+`"`)
			if err := VerifyAttestation(context.Background(), path, testRepo, testWorkflow); err != nil {
				t.Fatalf("VerifyAttestation = %v, want nil", err)
			}
			data, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			want := []string{"attestation", "verify", path, "--repo", testRepo, "--signer-workflow", testWorkflow}
			if !slices.Equal(got, want) {
				t.Errorf("args = %q, want %q", got, want)
			}
		})
	}
}

// gh's output reaches the error with its escape sequences removed.
func TestVerifyAttestationFails(t *testing.T) {
	fakeGH(t, `printf 'bad attestation\033[31m red\n' >&2; exit 1`)
	err := VerifyAttestation(context.Background(), "/tmp/x/checksums.txt", testRepo, testWorkflow)
	if err == nil {
		t.Fatal("VerifyAttestation = nil for a gh that exits 1")
	}
	if !strings.Contains(err.Error(), "bad attestation") {
		t.Errorf("error lacks gh's output: %q", err)
	}
	if strings.ContainsRune(err.Error(), 0x1b) {
		t.Errorf("error holds an ESC byte: %q", err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Errorf("error = %v, want it to wrap exit status 1", err)
	}
}

func TestVerifyAttestationCapsOutput(t *testing.T) {
	fakeGH(t, `head -c 1000000 /dev/zero | tr '\0' x; exit 1`)
	err := VerifyAttestation(context.Background(), "/tmp/x/checksums.txt", testRepo, testWorkflow)
	if err == nil {
		t.Fatal("VerifyAttestation = nil for a gh that exits 1")
	}
	if n := len(err.Error()); n > maxGHOutput+200 {
		t.Errorf("error is %d bytes, want at most %d", n, maxGHOutput+200)
	}
	if !strings.Contains(err.Error(), "xxxx") {
		t.Errorf("error lacks gh's output: %.200q", err)
	}
}

func TestVerifyAttestationNoGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := VerifyAttestation(context.Background(), "/tmp/x/checksums.txt", testRepo, testWorkflow)
	if !errors.Is(err, ErrNoGH) {
		t.Errorf("VerifyAttestation with no gh on PATH = %v, want ErrNoGH", err)
	}
}

// A gh in the current directory reached through "." on PATH is refused
// and never runs.
func TestVerifyAttestationRefusesDotPath(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	dir := fakeGH(t, `touch "`+marker+`"`)
	t.Chdir(dir)
	t.Setenv("PATH", ".")
	err := VerifyAttestation(context.Background(), "/tmp/x/checksums.txt", testRepo, testWorkflow)
	if !errors.Is(err, exec.ErrDot) {
		t.Errorf("VerifyAttestation via . on PATH = %v, want exec.ErrDot", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the gh found through . ran")
	}
}

func TestVerifyAttestationContextCancel(t *testing.T) {
	fakeGH(t, `exec sleep 60`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if err := VerifyAttestation(ctx, "/tmp/x/checksums.txt", testRepo, testWorkflow); err == nil {
		t.Fatal("VerifyAttestation = nil after the deadline")
	}
	if d := time.Since(begin); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
