package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sentiolabs/selfupdate-go"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
)

// --- Contract assertions ---

var (
	_ func(string, string, io.Writer, io.Writer, io.Reader) *selfupdate.Updater = New
	_ func() []selfupdate.ManagedInstall                                        = Managed
)

// --- Helpers ---

const (
	oldBinary = "old tilt-tui\n"
	newBinary = "new tilt-tui\n"
	newTag    = "v0.5.0"
)

// tarGz returns a .tar.gz holding a tilt-tui file and a LICENSE, like a
// release archive.
func tarGz(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{
		{"tilt-tui", newBinary},
		{"LICENSE", "license\n"},
	} {
		hdr := &tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHub serves the releases API for SentioLabs/go-tilt-tui with one
// release, newTag, and its archive and checksums.txt.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	archiveName := fmt.Sprintf("go-tilt-tui_%s_%s_%s.tar.gz", strings.TrimPrefix(newTag, "v"), runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t)
	sum := sha256.Sum256(archive)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + archiveName + "\n")

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	release := map[string]any{
		"tag_name":   newTag,
		"prerelease": false,
		"draft":      false,
		"assets": []map[string]any{
			{"name": archiveName, "browser_download_url": srv.URL + "/download/" + archiveName, "size": len(archive)},
			{"name": "checksums.txt", "browser_download_url": srv.URL + "/download/checksums.txt", "size": len(checksums)},
		},
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Error(err)
		}
	}
	mux.HandleFunc("GET /repos/SentioLabs/go-tilt-tui/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, release)
	})
	mux.HandleFunc("GET /repos/SentioLabs/go-tilt-tui/releases", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []any{release})
	})
	mux.HandleFunc("GET /download/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("GET /download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(checksums)
	})
	return srv
}

// fakeGH writes an executable sh script called gh into a new temp dir and
// sets PATH to that dir alone. The script resets its own PATH.
func fakeGH(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// noGH sets PATH to an empty dir.
func noGH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// writeTarget writes oldBinary to path, creating its parents.
func writeTarget(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type harness struct {
	u           *selfupdate.Updater
	target      string
	out, errOut *bytes.Buffer
}

// newHarness builds an updater at v0.4.0 against a fake GitHub, replacing
// target.
func newHarness(t *testing.T, target string) harness {
	t.Helper()
	srv := fakeGitHub(t)
	h := harness{target: target, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	h.u = newUpdater(updaterConfig{
		dir:        t.TempDir(),
		version:    "v0.4.0",
		out:        h.out,
		errOut:     h.errOut,
		in:         strings.NewReader(""),
		baseURL:    srv.URL,
		targetPath: target,
	})
	return h
}

func (h harness) update(t *testing.T) error {
	t.Helper()
	return h.u.Update(context.Background(), selfupdate.UpdateOptions{Yes: true})
}

func (h harness) targetBody(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(h.target)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const noGHMessage = "install gh and run gh auth login, or set TILT_TUI_INSECURE_SKIP_ATTESTATION=1"

// --- Behavior tests ---

func TestNewWiresTiltTUI(t *testing.T) {
	dir := t.TempDir()
	out, errOut, in := &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("")
	u := New(dir, "v0.4.0", out, errOut, in)
	if u.Name != "tilt-tui" || u.Version != "v0.4.0" || u.Out != out || u.ErrOut != errOut || u.In != in {
		t.Errorf("New = %+v", u)
	}
	src, ok := u.Source.(*selfupdate.GitHubSource)
	if !ok || src.Owner != "SentioLabs" || src.Repo != "go-tilt-tui" || src.BaseURL != "" {
		t.Errorf("Source = %#v", u.Source)
	}
	inst, ok := u.Installer.(*selfupdate.ArchiveInstaller)
	if !ok {
		t.Fatalf("Installer = %T, want *selfupdate.ArchiveInstaller", u.Installer)
	}
	if inst.Name != "tilt-tui" || inst.AssetTemplate != "go-tilt-tui_{version}_{os}_{arch}.tar.gz" ||
		inst.TargetPath != "" || inst.Out != out || inst.VerifyChecksums == nil || inst.SkipChecksum {
		t.Errorf("Installer = %+v", inst)
	}
	if err := u.Store.SetChannel(selfupdate.ChannelRC); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ChannelFile)); err != nil {
		t.Errorf("Store doesn't write %s in dir: %v", ChannelFile, err)
	}
}

// stdStreams points os.Stdout and os.Stderr at temp files for the test
// and returns a func that reads what each one got.
func stdStreams(t *testing.T) func() (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	open := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	stdout, stderr := open("stdout"), open("stderr")
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	t.Cleanup(func() { os.Stdout, os.Stderr = origOut, origErr })
	return func() (string, string) {
		read := func(f *os.File) string {
			data, err := os.ReadFile(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
		return read(stdout), read(stderr)
	}
}

// New treats nil writers as the standard streams, so the hook can write
// on both its paths.
func TestNilWritersUseStdStreams(t *testing.T) {
	for _, tc := range []struct {
		name, stream, want string
		setup              func(t *testing.T)
	}{
		{"skip", "stderr", "skipping attestation check", func(t *testing.T) {
			noGH(t)
			t.Setenv(SkipEnv, "1")
		}},
		{"verified", "stdout", "Verified checksums.txt attestation", func(t *testing.T) {
			fakeGH(t, `exit 0`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			read := stdStreams(t)
			srv := fakeGitHub(t)
			target := writeTarget(t, filepath.Join(t.TempDir(), "tilt-tui"))
			u := newUpdater(updaterConfig{
				dir:        t.TempDir(),
				version:    "v0.4.0",
				baseURL:    srv.URL,
				targetPath: target,
			})
			if u.Out != os.Stdout || u.ErrOut != os.Stderr || u.In != nil {
				t.Errorf("Out = %v, ErrOut = %v, In = %v", u.Out, u.ErrOut, u.In)
			}
			if inst := u.Installer.(*selfupdate.ArchiveInstaller); inst.Out != os.Stdout {
				t.Errorf("Installer.Out = %v, want os.Stdout", inst.Out)
			}
			if err := u.Update(context.Background(), selfupdate.UpdateOptions{Yes: true}); err != nil {
				t.Fatalf("Update = %v", err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != newBinary {
				t.Errorf("target = %q, want %q", data, newBinary)
			}
			stdout, stderr := read()
			got := map[string]string{"stdout": stdout, "stderr": stderr}[tc.stream]
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s lacks %q:\n%s", tc.stream, tc.want, got)
			}
		})
	}
}

func TestUpdateVerifiedByGH(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	fakeGH(t, `printf '%s\n' "$@" > "`+argsFile+`"`)
	h := newHarness(t, writeTarget(t, filepath.Join(t.TempDir(), "bin", "tilt-tui")))
	if err := h.update(t); err != nil {
		t.Fatalf("Update = %v\nout:\n%s\nerrOut:\n%s", err, h.out, h.errOut)
	}
	if got := h.targetBody(t); got != newBinary {
		t.Errorf("target = %q, want %q", got, newBinary)
	}
	if !strings.Contains(h.out.String(), "Verified checksums.txt attestation") {
		t.Errorf("out lacks the attestation line:\n%s", h.out)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(args) != 7 {
		t.Fatalf("gh args = %q", args)
	}
	if !filepath.IsAbs(args[2]) || !strings.HasSuffix(args[2], "/verify/checksums.txt") {
		t.Errorf("gh path arg = %q, want an absolute .../verify/checksums.txt", args[2])
	}
	want := []string{"attestation", "verify", args[2], "--repo", Repo, "--signer-workflow", SignerWorkflow}
	if !slices.Equal(args, want) {
		t.Errorf("gh args = %q, want %q", args, want)
	}
	if Repo != "SentioLabs/go-tilt-tui" || SignerWorkflow != "SentioLabs/go-tilt-tui/.github/workflows/release.yml" {
		t.Errorf("Repo = %q, SignerWorkflow = %q", Repo, SignerWorkflow)
	}
}

func TestUpdateRefusedByGH(t *testing.T) {
	fakeGH(t, `echo 'no attestations found' >&2; exit 1`)
	h := newHarness(t, writeTarget(t, filepath.Join(t.TempDir(), "tilt-tui")))
	err := h.update(t)
	if err == nil {
		t.Fatal("Update = nil for a gh that exits 1")
	}
	if !strings.Contains(err.Error(), "no attestations found") {
		t.Errorf("error lacks gh's output: %q", err)
	}
	if got := h.targetBody(t); got != oldBinary {
		t.Errorf("target = %q, want it unchanged", got)
	}
}

func TestUpdateWithoutGH(t *testing.T) {
	noGH(t)
	h := newHarness(t, writeTarget(t, filepath.Join(t.TempDir(), "tilt-tui")))
	err := h.update(t)
	if err == nil || !strings.Contains(err.Error(), noGHMessage) {
		t.Fatalf("Update with no gh = %v, want it to say %q", err, noGHMessage)
	}
	if !errors.Is(err, proc.ErrNoGH) {
		t.Errorf("Update with no gh = %v, want it to wrap proc.ErrNoGH", err)
	}
	if got := h.targetBody(t); got != oldBinary {
		t.Errorf("target = %q, want it unchanged", got)
	}
}

func TestUpdateSkipAttestation(t *testing.T) {
	noGH(t)
	t.Setenv(SkipEnv, "1")
	h := newHarness(t, writeTarget(t, filepath.Join(t.TempDir(), "tilt-tui")))
	if err := h.update(t); err != nil {
		t.Fatalf("Update with %s=1 = %v", SkipEnv, err)
	}
	if !strings.Contains(h.errOut.String(), "skipping attestation check (TILT_TUI_INSECURE_SKIP_ATTESTATION=1)") {
		t.Errorf("errOut lacks the skip warning:\n%s", h.errOut)
	}
	if got := h.targetBody(t); got != newBinary {
		t.Errorf("target = %q, want %q", got, newBinary)
	}
}

func TestUpdateSkipNeedsExactlyOne(t *testing.T) {
	for _, v := range []string{"true", "yes", "01", " 1", "1 "} {
		t.Run(v, func(t *testing.T) {
			noGH(t)
			t.Setenv(SkipEnv, v)
			h := newHarness(t, writeTarget(t, filepath.Join(t.TempDir(), "tilt-tui")))
			err := h.update(t)
			if err == nil || !strings.Contains(err.Error(), noGHMessage) {
				t.Fatalf("Update with %s=%q = %v, want the gh error", SkipEnv, v, err)
			}
			if strings.Contains(h.errOut.String(), "skipping") {
				t.Errorf("errOut has a skip warning:\n%s", h.errOut)
			}
			if got := h.targetBody(t); got != oldBinary {
				t.Errorf("target = %q, want it unchanged", got)
			}
		})
	}
}

func TestUpdateRefusesMise(t *testing.T) {
	fakeGH(t, `exit 0`)
	target := filepath.Join(t.TempDir(), ".local", "share", "mise", "installs",
		"github-sentiolabs-go-tilt-tui", "0.4.0", "tilt-tui")
	h := newHarness(t, writeTarget(t, target))
	err := h.update(t)
	if !errors.Is(err, selfupdate.ErrManagedInstall) {
		t.Fatalf("Update under mise = %v, want ErrManagedInstall", err)
	}
	if !strings.Contains(err.Error(), "mise upgrade github:SentioLabs/go-tilt-tui") {
		t.Errorf("error lacks the mise hint: %q", err)
	}
	if got := h.targetBody(t); got != oldBinary {
		t.Errorf("target = %q, want it unchanged", got)
	}
}

// The hook hands gh an absolute path, so a relative path that starts with
// "-" can't be read as a gh flag.
func TestVerifyChecksumsAbsolutePath(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	fakeGH(t, `printf '%s\n' "$@" > "`+argsFile+`"`)
	cwd := t.TempDir()
	t.Chdir(cwd)
	h := newHarness(t, "")
	verify := h.u.Installer.(*selfupdate.ArchiveInstaller).VerifyChecksums
	if err := verify(context.Background(), selfupdate.Release{Tag: newTag}, "-checksums.txt"); err != nil {
		t.Fatalf("VerifyChecksums = %v", err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(args) < 3 || args[2] != filepath.Join(cwd, "-checksums.txt") {
		t.Errorf("gh args = %q, want the path %s", args, filepath.Join(cwd, "-checksums.txt"))
	}
}

func TestManaged(t *testing.T) {
	defaults := slices.Clone(selfupdate.DefaultManagedInstalls)
	m := Managed()
	managers := make([]string, 0, len(m))
	for _, e := range m {
		managers = append(managers, e.Manager)
	}
	for _, d := range defaults {
		if !slices.Contains(managers, d.Manager) {
			t.Errorf("Managed() lacks %s", d.Manager)
		}
	}
	i := slices.Index(managers, "mise")
	if i < 0 {
		t.Fatalf("Managed() = %q, want mise", managers)
	}
	if m[i].Hint != "mise upgrade github:SentioLabs/go-tilt-tui" {
		t.Errorf("mise hint = %q", m[i].Hint)
	}
	if !m[i].Pattern.MatchString("/home/u/.local/share/mise/installs/x/1/tilt-tui") {
		t.Errorf("mise pattern %s doesn't match a mise install", m[i].Pattern)
	}
	if len(selfupdate.DefaultManagedInstalls) != len(defaults) {
		t.Fatalf("DefaultManagedInstalls grew to %d entries", len(selfupdate.DefaultManagedInstalls))
	}
	m[0].Manager = "changed"
	if !slices.EqualFunc(selfupdate.DefaultManagedInstalls, defaults, func(a, b selfupdate.ManagedInstall) bool {
		return a.Manager == b.Manager && a.Pattern == b.Pattern && a.Hint == b.Hint
	}) {
		t.Errorf("DefaultManagedInstalls changed: %+v", selfupdate.DefaultManagedInstalls)
	}
}
