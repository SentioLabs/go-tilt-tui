// Package version reports the build's version, commit and date.
package version

import (
	"fmt"
	"runtime/debug"
)

const unknown = "unknown"

// Release builds set these with -ldflags -X. See .goreleaser.yaml.
var (
	Version   = "dev"
	GitCommit = unknown
	BuildDate = unknown
)

func init() { fill(debug.ReadBuildInfo) }

// fill sets any value the linker left at its default from the
// module and VCS data the Go toolchain stamps into the binary.
// A `go build` from a git checkout stamps vcs.revision and vcs.time.
// A tagged module build would stamp only Main.Version.
func fill(read func() (*debug.BuildInfo, bool)) {
	info, ok := read()
	if !ok {
		return
	}
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch {
		case s.Key == "vcs.revision" && GitCommit == unknown:
			GitCommit = s.Value
		case s.Key == "vcs.time" && BuildDate == unknown:
			BuildDate = s.Value
		}
	}
	if len(GitCommit) > 12 {
		GitCommit = GitCommit[:12]
	}
}

// String returns "tilt-tui <Version> (<GitCommit>, <BuildDate>)".
func String() string {
	return fmt.Sprintf("tilt-tui %s (%s, %s)", Version, GitCommit, BuildDate)
}
