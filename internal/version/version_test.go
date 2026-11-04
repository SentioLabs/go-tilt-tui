package version

import (
	"runtime/debug"
	"testing"
)

// --- Contract assertions ---
// goreleaser ldflags, the Makefile and --version depend on these.
var (
	_ string        = Version
	_ string        = GitCommit
	_ string        = BuildDate
	_ func() string = String
)

// --- Behavior tests ---

func reset(t *testing.T, v, c, d string) {
	t.Helper()
	oldV, oldC, oldD := Version, GitCommit, BuildDate
	Version, GitCommit, BuildDate = v, c, d
	t.Cleanup(func() { Version, GitCommit, BuildDate = oldV, oldC, oldD })
}

func info(mainVersion string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		bi := &debug.BuildInfo{Settings: settings}
		bi.Main.Version = mainVersion
		return bi, true
	}
}

func TestFillFromVCS(t *testing.T) {
	reset(t, "dev", unknown, unknown)
	fill(info("(devel)",
		debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		debug.BuildSetting{Key: "vcs.time", Value: "2026-11-03T10:00:00Z"}))
	if Version != "dev" || GitCommit != "0123456789ab" || BuildDate != "2026-11-03T10:00:00Z" {
		t.Fatalf("got %q %q %q", Version, GitCommit, BuildDate)
	}
}

func TestFillKeepsLinkerValues(t *testing.T) {
	reset(t, "v0.1.0", "abc123", "2026-11-01")
	fill(info("v9.9.9",
		debug.BuildSetting{Key: "vcs.revision", Value: "ffffffffffffffff"},
		debug.BuildSetting{Key: "vcs.time", Value: "2000-01-01T00:00:00Z"}))
	if Version != "v0.1.0" || GitCommit != "abc123" || BuildDate != "2026-11-01" {
		t.Fatalf("got %q %q %q", Version, GitCommit, BuildDate)
	}
}

func TestFillModuleVersion(t *testing.T) {
	reset(t, "dev", unknown, unknown)
	fill(info("v0.2.0"))
	if Version != "v0.2.0" || GitCommit != unknown || BuildDate != unknown {
		t.Fatalf("got %q %q %q", Version, GitCommit, BuildDate)
	}
}

func TestFillNoBuildInfo(t *testing.T) {
	reset(t, "dev", unknown, unknown)
	fill(func() (*debug.BuildInfo, bool) { return nil, false })
	if Version != "dev" || GitCommit != unknown || BuildDate != unknown {
		t.Fatalf("got %q %q %q", Version, GitCommit, BuildDate)
	}
}

func TestString(t *testing.T) {
	reset(t, "v0.1.0", "abc123", "2026-11-03")
	if got, want := String(), "tilt-tui v0.1.0 (abc123, 2026-11-03)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
