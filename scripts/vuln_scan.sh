#!/usr/bin/env bash
#
# Dependency and standard library vulnerability scan (osv-scanner).
# Shared by `make vuln` and .github/workflows/ci.yml so a developer runs the
# same flags as CI.
#
# Usage:
#   scripts/vuln_scan.sh              # gating scan, once per release target
#   scripts/vuln_scan.sh --inventory  # every advisory, informational, never fails
#
# osv-scanner queries OSV (a superset of the Go vulnerability database) and
# runs govulncheck's call analysis, hiding advisories it proves unreachable.
# Call analysis only sees one GOOS/GOARCH, so the gating scan runs once for
# each target goreleaser builds.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

CONFIG="osv-scanner.toml"
TARGETS=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)
MODE="gate"

if [ "$#" -gt 1 ]; then
  echo "Error: expected at most one argument, got $#" >&2
  exit 2
fi

case "${1:-}" in
  --inventory) MODE="inventory" ;;
  -h|--help)   sed -n '3,14p' "${BASH_SOURCE[0]}"; exit 0 ;;
  "")          ;;
  *)           echo "Error: unknown argument '$1' (expected --inventory)" >&2; exit 2 ;;
esac

# mise installs both at the versions pinned in mise.toml, locally and in CI.
for tool in osv-scanner go; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Error: $tool not found on PATH. Run 'mise install', then activate mise in this shell (eval \"\$(mise activate zsh)\")." >&2
    exit 127
  fi
done

# Call analysis judges stdlib advisories at GoVersionOverride. That must be
# the Go that builds the release.
override="$(sed -n 's/^GoVersionOverride *= *"\(.*\)"/\1/p' "$CONFIG")"
installed="$(go env GOVERSION)"
installed="${installed#go}"
if [ "$override" != "$installed" ]; then
  echo "ERROR: GoVersionOverride in $CONFIG is '$override' but the installed Go is '$installed'." >&2
  echo "       Bump GoVersionOverride in osv-scanner.toml with the Go version in mise.toml." >&2
  exit 1
fi

# Call analysis loads every package, so it needs a populated module cache.
go mod download

# Progress chatter from the filesystem walk. Not useful in logs or summaries.
strip_noise() { grep -vE '^(Starting filesystem|Scanned|End status)' || true; }

# Appends to the GitHub step summary under Actions. No-op locally.
summarize() {
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  {
    echo "## $1"
    echo
    echo '```'
    strip_noise < "$2"
    echo '```'
  } >> "$GITHUB_STEP_SUMMARY"
}

if [ "$MODE" = "inventory" ]; then
  # Lists what the gating scan hides as unreachable, so those findings stay
  # visible for triage without blocking anyone. Never fails.
  output="osv-output-inventory.txt"
  set +e
  osv-scanner scan source --lockfile=go.mod --config="$CONFIG" --all-vulns >"$output" 2>&1
  set -e
  strip_noise < "$output"
  summarize "All known advisories (informational, does not gate)" "$output"
  exit 0
fi

failed=()
for t in "${TARGETS[@]}"; do
  goos="${t%/*}"
  goarch="${t#*/}"
  output="osv-output-$goos-$goarch.txt"
  echo "==> osv-scanner for $goos/$goarch"

  set +e
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    osv-scanner scan source --lockfile=go.mod --config="$CONFIG" >"$output" 2>&1
  code=$?
  set -e

  strip_noise < "$output"
  summarize "osv-scanner $t" "$output"

  # Without Go or a module cache, osv-scanner logs one of these and reports
  # every advisory, including the ones call analysis would prove unreachable.
  if grep -qE 'Failed to run code analysis|Skipping call analysis' "$output"; then
    echo "ERROR: call analysis did not run for $t (needs Go on PATH and a populated module cache). Results include unreachable findings and can't be trusted." >&2
    failed+=("$t")
    continue
  fi

  case "$code" in
    0)
      echo "no actionable vulnerabilities for $t"
      ;;
    1)
      echo "ERROR: actionable vulnerabilities for $t" >&2
      failed+=("$t")
      ;;
    *)
      echo "ERROR: osv-scanner exited $code for $t (scanner error)" >&2
      failed+=("$t")
      ;;
  esac
done

if [ "${#failed[@]}" -gt 0 ]; then
  echo "ERROR: vulnerability scan failed for: ${failed[*]}" >&2
  echo "       Upgrade the affected module or Go, or add a justified entry to $CONFIG." >&2
  exit 1
fi
