#!/usr/bin/env bash
# Installs tilt-tui from a GitHub release. It checks the archive's sha256
# against checksums.txt, then its GitHub artifact attestation with gh.
#
#   TILT_TUI_VERSION      tag to install, for example v0.2.0 (default: latest)
#   TILT_TUI_INSTALL_DIR  where to put tilt-tui (default: $HOME/.local/bin)
#   TILT_TUI_INSECURE_SKIP_ATTESTATION=1
#                         skip the gh attestation check (sha256 still runs)
#   TILT_TUI_BASE_URL     release host (default: https://github.com/SentioLabs/go-tilt-tui)
set -euo pipefail

REPO="SentioLabs/go-tilt-tui"
SIGNER="SentioLabs/go-tilt-tui/.github/workflows/release.yml"
BASE_URL="${TILT_TUI_BASE_URL:-https://github.com/$REPO}"

die()  { echo "install.sh: $*" >&2; exit 1; }
warn() { echo "install.sh: warning: $*" >&2; }

[ -n "${TILT_TUI_INSTALL_DIR:-}" ] || [ -n "${HOME:-}" ] ||
  die "HOME is not set. Set TILT_TUI_INSTALL_DIR."
INSTALL_DIR="${TILT_TUI_INSTALL_DIR:-$HOME/.local/bin}"
# Trailing slashes would break the PATH check, but a bare / stays.
while [ "$INSTALL_DIR" != / ] && [ "${INSTALL_DIR%/}" != "$INSTALL_DIR" ]; do
  INSTALL_DIR="${INSTALL_DIR%/}"
done

work="$(mktemp -d)"
staged=""
cleanup() { rm -rf "$work"; if [ -n "$staged" ]; then rm -f "$staged"; fi; }
trap cleanup EXIT

# The whole script runs from main, so a download cut short by curl | bash
# runs nothing.
main() {
  local os arch tag latest re version archive url hint
  local count expected actual gh_out found

  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) os="$(uname -s)" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) arch="$(uname -m)" ;;
  esac
  case "$os/$arch" in
    linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64) ;;
    *) die "unsupported platform $os/$arch: tilt-tui ships for linux and darwin on amd64 and arm64" ;;
  esac

  # The tag becomes part of a URL and a file name, so it's checked before
  # any download.
  latest=0
  tag="${TILT_TUI_VERSION:-}"
  if [ -z "$tag" ]; then
    latest=1
    tag="$(curl -fsSI "$BASE_URL/releases/latest" | tr -d '\r' |
      awk 'tolower($1) == "location:" { loc = $2 } END { print loc }')" ||
      die "could not find the latest release at $BASE_URL/releases/latest"
    tag="${tag##*/}"
  fi
  re='^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'
  [[ "$tag" =~ $re ]] || die "bad release tag '$tag'"

  version="${tag#v}"
  archive="go-tilt-tui_${version}_${os}_${arch}.tar.gz"
  url="$BASE_URL/releases/download/$tag"
  hint=""
  if [ "$latest" = 1 ]; then
    hint=" (a new release takes a few minutes to publish its files, so retry shortly)"
  fi
  curl -fsSL -o "$work/$archive" "$url/$archive" ||
    die "download failed: $url/$archive$hint"
  curl -fsSL -o "$work/checksums.txt" "$url/checksums.txt" ||
    die "download failed: $url/checksums.txt$hint"

  # The sha256 check always runs, even when the attestation check is skipped.
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$work/$archive" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$work/$archive" | awk '{ print $1 }')"
  else
    die "need sha256sum or shasum to check the download"
  fi
  count="$(awk -v f="$archive" '$2 == f || $2 == "*" f { n++ } END { print n + 0 }' "$work/checksums.txt")"
  [ "$count" = 1 ] ||
    die "checksums.txt must list $archive exactly once, found $count lines"
  expected="$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$work/checksums.txt")"
  [ "$expected" = "$actual" ] ||
    die "sha256 mismatch for $archive: checksums.txt has $expected, the download has $actual"

  if [ "${TILT_TUI_INSECURE_SKIP_ATTESTATION:-}" = 1 ]; then
    warn "skipping the attestation check (TILT_TUI_INSECURE_SKIP_ATTESTATION=1). Only the sha256 was checked."
  elif ! command -v gh >/dev/null 2>&1; then
    die "gh is needed to verify the release attestation: https://cli.github.com (or set TILT_TUI_INSECURE_SKIP_ATTESTATION=1 to skip it)"
  else
    gh_out="$(gh attestation verify "$work/$archive" --repo "$REPO" --signer-workflow "$SIGNER" </dev/null 2>&1)" || {
      printf '%s\n' "$gh_out" >&2
      die "attestation check failed for $archive (is gh logged in? run gh auth login)"
    }
  fi

  tar -xzf "$work/$archive" -C "$work" tilt-tui ||
    die "could not extract tilt-tui from $archive"
  if [ ! -f "$work/tilt-tui" ] || [ -L "$work/tilt-tui" ]; then
    die "$archive does not hold a regular tilt-tui file"
  fi

  # mv would move the staged file into a directory, so refuse one.
  [ ! -d "$INSTALL_DIR/tilt-tui" ] ||
    die "$INSTALL_DIR/tilt-tui is a directory. Remove it or set TILT_TUI_INSTALL_DIR."

  # The rename keeps a running old binary intact.
  mkdir -p "$INSTALL_DIR"
  staged="$(mktemp "$INSTALL_DIR/.tilt-tui.XXXXXX")"
  cp "$work/tilt-tui" "$staged"
  chmod 0755 "$staged"
  mv -f "$staged" "$INSTALL_DIR/tilt-tui"
  staged=""

  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) warn "$INSTALL_DIR is not on PATH. Add it to run tilt-tui by name." ;;
  esac
  found="$(command -v tilt-tui || true)"
  if [ -n "$found" ] && ! [ "$found" -ef "$INSTALL_DIR/tilt-tui" ]; then
    warn "tilt-tui on PATH is $found, not $INSTALL_DIR/tilt-tui. Remove the old one or put $INSTALL_DIR first."
  fi

  "$INSTALL_DIR/tilt-tui" --version </dev/null
}

main "$@"
