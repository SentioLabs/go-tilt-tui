package proc

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sentiolabs/go-tilt-tui/internal/termsafe"
)

// ErrNoGH is returned when gh isn't on PATH.
var ErrNoGH = errors.New("gh not found on PATH")

// maxGHOutput bounds how much of gh's output a failure error carries.
const maxGHOutput = 4 << 10

// VerifyAttestation runs `gh attestation verify path --repo repo
// --signer-workflow workflow` and returns nil when gh exits 0. A failure
// returns an error carrying gh's combined output, capped and passed
// through termsafe.Text.
func VerifyAttestation(ctx context.Context, path, repo, workflow string) error {
	gh, err := exec.LookPath("gh")
	if err != nil && !errors.Is(err, exec.ErrDot) {
		// A bare name fails LookPath only with exec.ErrNotFound.
		return fmt.Errorf("gh attestation verify: %w", ErrNoGH)
	}
	if err != nil {
		// exec.ErrDot: PATH named the current directory. A gh found
		// there could be planted by the directory's owner.
		return fmt.Errorf("gh attestation verify: %w", err)
	}
	// The absolute path keeps exec from searching PATH a second time.
	gh, err = filepath.Abs(gh)
	if err != nil {
		return fmt.Errorf("gh attestation verify: %w", err)
	}
	// The environment is inherited, so GH_TOKEN and gh's login apply.
	cmd := exec.CommandContext(ctx, gh,
		"attestation", "verify", path, "--repo", repo, "--signer-workflow", workflow)
	out := &capped{max: maxGHOutput}
	// One writer for both streams: exec then calls Write from one
	// goroutine at a time.
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh attestation verify: %w: %s", err, termsafe.Text(strings.TrimSpace(string(out.buf))))
	}
	return nil
}
