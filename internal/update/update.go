// Package update builds tilt-tui's self updater. It keeps the release
// channel in selfupdate.toml and checks each release's checksums.txt with
// gh attestation verify before trusting it.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/sentiolabs/selfupdate-go"

	"github.com/sentiolabs/go-tilt-tui/internal/proc"
)

const (
	// Repo is the GitHub repository releases come from.
	Repo = "SentioLabs/go-tilt-tui"
	// SignerWorkflow is the workflow that must have signed checksums.txt.
	SignerWorkflow = "SentioLabs/go-tilt-tui/.github/workflows/release.yml"
	// ChannelFile is the file in the config dir that holds the channel.
	ChannelFile = "selfupdate.toml"
	// SkipEnv set to "1" skips the attestation check.
	SkipEnv = "TILT_TUI_INSECURE_SKIP_ATTESTATION"
)

// updaterConfig holds what New takes plus the overrides tests need.
type updaterConfig struct {
	dir, version string
	out, errOut  io.Writer
	in           io.Reader
	baseURL      string // GitHub API base, "" means api.github.com
	targetPath   string // binary to replace, "" means the running one
}

// New returns the updater for the running tilt-tui. dir is config.Dir().
// A nil out or errOut means os.Stdout or os.Stderr.
func New(dir, version string, out, errOut io.Writer, in io.Reader) *selfupdate.Updater {
	return newUpdater(updaterConfig{dir: dir, version: version, out: out, errOut: errOut, in: in})
}

// newUpdater builds the updater New returns, with cfg's test overrides.
// It swaps nil writers for the standard streams once, so the hook and the
// library write to the same ones.
func newUpdater(cfg updaterConfig) *selfupdate.Updater {
	if cfg.out == nil {
		cfg.out = os.Stdout
	}
	if cfg.errOut == nil {
		cfg.errOut = os.Stderr
	}
	return &selfupdate.Updater{
		Name:    "tilt-tui",
		Version: cfg.version,
		Source:  &selfupdate.GitHubSource{Owner: "SentioLabs", Repo: "go-tilt-tui", BaseURL: cfg.baseURL},
		Store:   ChannelStore(cfg.dir),
		Installer: &selfupdate.ArchiveInstaller{
			Name:            "tilt-tui",
			AssetTemplate:   "go-tilt-tui_{version}_{os}_{arch}.tar.gz",
			Managed:         Managed(),
			TargetPath:      cfg.targetPath,
			Out:             cfg.out,
			VerifyChecksums: verifier(cfg.out, cfg.errOut),
		},
		Out:    cfg.out,
		ErrOut: cfg.errOut,
		In:     cfg.in,
	}
}

// verifier returns the VerifyChecksums hook. It runs gh attestation
// verify on the checksum file unless SkipEnv is exactly "1".
func verifier(out, errOut io.Writer) func(context.Context, selfupdate.Release, string) error {
	return func(ctx context.Context, _ selfupdate.Release, path string) error {
		if os.Getenv(SkipEnv) == "1" {
			_, _ = fmt.Fprintln(errOut, "skipping attestation check (TILT_TUI_INSECURE_SKIP_ATTESTATION=1)")
			return nil
		}
		// An absolute path can't start with "-", so gh never reads it
		// as a flag.
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		err = proc.VerifyAttestation(ctx, abs, Repo, SignerWorkflow)
		if errors.Is(err, proc.ErrNoGH) {
			return fmt.Errorf("self update verifies the release with gh: "+
				"install gh and run gh auth login, or set %s=1: %w", SkipEnv, err)
		}
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out, "Verified checksums.txt attestation")
		return nil
	}
}

// Managed is selfupdate.DefaultManagedInstalls plus mise. It returns a
// new slice on every call.
func Managed() []selfupdate.ManagedInstall {
	return append(slices.Clone(selfupdate.DefaultManagedInstalls), selfupdate.ManagedInstall{
		Pattern: regexp.MustCompile(`/mise/installs/`),
		Manager: "mise",
		Hint:    "mise upgrade github:SentioLabs/go-tilt-tui",
	})
}
