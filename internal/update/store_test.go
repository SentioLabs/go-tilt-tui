package update

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/sentiolabs/selfupdate-go"
)

// --- Contract assertions ---

var _ func(string) selfupdate.Store = ChannelStore

// --- Behavior tests ---

// dirNames returns the sorted names of the entries in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func TestChannelStoreMissingFile(t *testing.T) {
	c, err := ChannelStore(t.TempDir()).Channel()
	if err != nil || c != "" {
		t.Fatalf("Channel() with no file = %q, %v, want \"\", nil", c, err)
	}
}

func TestChannelStoreSetChannel(t *testing.T) {
	dir := t.TempDir()
	s := ChannelStore(dir)
	if err := s.SetChannel(selfupdate.ChannelRC); err != nil {
		t.Fatalf("SetChannel = %v", err)
	}
	path := filepath.Join(dir, ChannelFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("%s mode = %o, want 600", ChannelFile, mode)
	}
	var f struct {
		Channel string `toml:"channel"`
	}
	if _, err := toml.DecodeFile(path, &f); err != nil {
		t.Fatal(err)
	}
	if f.Channel != "rc" {
		t.Errorf("file channel = %q, want rc", f.Channel)
	}
	c, err := s.Channel()
	if err != nil || c != selfupdate.ChannelRC {
		t.Errorf("Channel() = %q, %v, want rc", c, err)
	}
}

// A second write replaces the first through the same temp file and rename.
func TestChannelStoreOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := ChannelStore(dir)
	for _, c := range []selfupdate.Channel{selfupdate.ChannelNightly, selfupdate.ChannelStable} {
		if err := s.SetChannel(c); err != nil {
			t.Fatalf("SetChannel(%q) = %v", c, err)
		}
	}
	if c, err := s.Channel(); err != nil || c != selfupdate.ChannelStable {
		t.Errorf("Channel() = %q, %v, want stable", c, err)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{ChannelFile}) {
		t.Errorf("dir holds %q, want only %s", got, ChannelFile)
	}
}

func TestChannelStoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tilt-tui")
	if err := ChannelStore(dir).SetChannel(selfupdate.ChannelRC); err != nil {
		t.Fatalf("SetChannel = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("dir mode = %o, want 700", mode)
	}
}

func TestChannelStoreLeavesConfigAlone(t *testing.T) {
	dir := t.TempDir()
	config := []byte("theme = \"mono\"\n# channel = \"nightly\"\n")
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ChannelStore(dir).SetChannel(selfupdate.ChannelRC); err != nil {
		t.Fatalf("SetChannel = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, config) {
		t.Errorf("config.toml changed:\n%s", got)
	}
}

func TestChannelStoreLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := ChannelStore(dir).SetChannel(selfupdate.ChannelRC); err != nil {
		t.Fatalf("SetChannel = %v", err)
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{ChannelFile}) {
		t.Errorf("dir holds %q, want only %s", got, ChannelFile)
	}
}

// A rename that fails removes the temp file. A directory named
// selfupdate.toml makes the rename fail.
func TestChannelStoreRemovesTempFileOnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ChannelFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ChannelStore(dir).SetChannel(selfupdate.ChannelRC); err == nil {
		t.Fatal("SetChannel over a directory = nil, want an error")
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{ChannelFile}) {
		t.Errorf("dir holds %q, want only %s", got, ChannelFile)
	}
}

func TestChannelStoreMalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ChannelFile)
	if err := os.WriteFile(path, []byte("channel = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ChannelStore(dir).Channel()
	if err == nil {
		t.Fatal("Channel() with a malformed file = nil, want an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q doesn't name %s", err, path)
	}
}
