#!/usr/bin/env bash
# Tests scripts/install.sh against a fake release served over file://.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
install="$here/install.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

tag=v0.1.0
archive=go-tilt-tui_0.1.0_linux_amd64.tar.gz
fails=0

# Fake release: <root>/release/releases/download/<tag>/{archive,checksums.txt}
rel="$root/release/releases/download/$tag"
mkdir -p "$rel" "$root/pkg" "$root/home"
printf '#!/bin/sh\necho "tilt-tui %s (abc123, 2026-11-03)"\n' "$tag" > "$root/pkg/tilt-tui"
chmod 0755 "$root/pkg/tilt-tui"
: > "$root/pkg/LICENSE"; : > "$root/pkg/README.md"
tar -czf "$rel/$archive" -C "$root/pkg" tilt-tui LICENSE README.md

# HIDE_SHA256SUM=1 leaves sha256sum out of the tool dir. On a host with
# shasum, that runs install.sh down its macOS path.
if [ "${HIDE_SHA256SUM:-}" != 1 ] && command -v sha256sum >/dev/null 2>&1; then
  sha_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  sha_tool=shasum
else
  echo "install_test: need sha256sum or shasum" >&2
  exit 1
fi

sha() {
  if [ "$sha_tool" = sha256sum ]; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

good_hash="$(sha "$rel/$archive")"
other_line="0000000000000000000000000000000000000000000000000000000000000000  go-tilt-tui_0.1.0_darwin_arm64.tar.gz"

# write_sums <line>... replaces checksums.txt with the given lines.
write_sums() {
  printf '%s\n' "$@" > "$rel/checksums.txt"
}
good_sums() {
  write_sums "$good_hash  $archive" "$other_line"
}
good_sums

# Restricted tool dir. It holds no gh and no uname.
mkdir -p "$root/tools" "$root/fakebin" "$root/ghbin"
for tool in bash curl tar gzip awk grep mktemp rm mkdir cp chmod mv tr tail cat head dirname env "$sha_tool"; do
  path="$(command -v "$tool")" || { echo "install_test: missing $tool" >&2; exit 1; }
  ln -s "$path" "$root/tools/$tool"
done

cat > "$root/fakebin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo Linux ;;
  -m) echo "${FAKE_ARCH:-x86_64}" ;;
  *) exit 1 ;;
esac
EOF
chmod 0755 "$root/fakebin/uname"

cat > "$root/ghbin/gh" <<EOF
#!/bin/sh
echo "\$@" >> "$root/gh.args"
if [ "\${FAKE_GH_EXIT:-0}" -ne 0 ]; then
  echo "fake gh: verification failed" >&2
fi
exit "\${FAKE_GH_EXIT:-0}"
EOF
chmod 0755 "$root/ghbin/gh"

# run <name> <want-exit> [VAR=value ...] runs install.sh in a clean env.
# NO_GH=1 leaves gh off PATH. OLD_BINARY=1 puts an old tilt-tui in the
# install dir first. It sets name, dir, rc, out and err for the checks
# below, and verdict reports the case.
problems=""
run() {
  name="$1"; want="$2"; shift 2
  dir="$(mktemp -d "$root/case.XXXXXX")/bin"
  out="$dir.out"; err="$dir.err"
  path="$root/fakebin:$root/ghbin:$root/tools"
  problems=""
  rm -f "$root/gh.args"
  for kv in "$@"; do
    case "$kv" in
      NO_GH=1) path="$root/fakebin:$root/tools" ;;
      OLD_BINARY=1) mkdir -p "$dir"; echo old > "$dir/tilt-tui" ;;
    esac
  done
  rc=0
  env -i HOME="$root/home" PATH="$path" \
    TILT_TUI_BASE_URL="file://$root/release" TILT_TUI_VERSION="$tag" \
    TILT_TUI_INSTALL_DIR="$dir" "$@" \
    "$(command -v bash)" "$install" > "$out" 2> "$err" || rc=$?
  if [ "$rc" -ne "$want" ]; then
    problems="$problems  exit $rc, want $want"$'\n'
  fi
}

problem() { problems="$problems  $*"$'\n'; }

expect_installed() {
  [ -x "$dir/tilt-tui" ] || problem "$dir/tilt-tui is not an executable file"
}
expect_not_installed() {
  [ ! -e "$dir/tilt-tui" ] || problem "$dir/tilt-tui exists"
}
# expect_in <file> <text> checks that the file holds the fixed string.
expect_in() {
  grep -qsF -- "$2" "$1" || problem "$(basename "$1") lacks: $2"
}
expect_no_gh_run() {
  [ ! -e "$root/gh.args" ] || problem "gh ran: $(cat "$root/gh.args")"
}

verdict() {
  local leftover
  if [ -d "$dir" ]; then
    for leftover in "$dir"/.tilt-tui.*; do
      [ ! -e "$leftover" ] || problem "temp file left: $leftover"
    done
  fi
  if [ -z "$problems" ]; then
    echo "ok   $name"
    return
  fi
  fails=$((fails + 1))
  echo "FAIL $name"
  printf '%s' "$problems"
  echo "  --- stdout"; sed 's/^/  /' "$out"
  echo "  --- stderr"; sed 's/^/  /' "$err"
}

run "good install" 0
expect_installed
expect_in "$out" "tilt-tui v0.1.0"
expect_in "$root/gh.args" "attestation verify"
expect_in "$root/gh.args" "--repo SentioLabs/go-tilt-tui"
expect_in "$root/gh.args" "--signer-workflow SentioLabs/go-tilt-tui/.github/workflows/release.yml"
verdict

write_sums "1111111111111111111111111111111111111111111111111111111111111111  $archive" "$other_line"
run "checksum mismatch" 1
expect_not_installed
expect_in "$err" "sha256 mismatch"
expect_no_gh_run
verdict
good_sums

write_sums "$other_line"
run "archive missing from checksums.txt" 1
expect_not_installed
expect_in "$err" "exactly once, found 0"
expect_no_gh_run
verdict
good_sums

write_sums "$good_hash  $archive" "$other_line" "$good_hash  $archive"
run "archive listed twice in checksums.txt" 1
expect_not_installed
expect_in "$err" "exactly once, found 2"
expect_no_gh_run
verdict
good_sums

run "gh missing" 1 NO_GH=1
expect_not_installed
expect_in "$err" "TILT_TUI_INSECURE_SKIP_ATTESTATION"
expect_in "$err" "https://cli.github.com"
verdict

run "gh verify fails" 1 FAKE_GH_EXIT=1
expect_not_installed
expect_in "$err" "fake gh: verification failed"
verdict

run "opt-out with gh missing" 0 NO_GH=1 TILT_TUI_INSECURE_SKIP_ATTESTATION=1
expect_installed
expect_in "$err" "warning: skipping the attestation check"
verdict

run "unsupported architecture" 1 FAKE_ARCH=riscv64
expect_not_installed
expect_in "$err" "linux and darwin on amd64 and arm64"
verdict

run "bad tag" 1 TILT_TUI_VERSION='v0.1.0/../../x'
expect_not_installed
expect_in "$err" "bad release tag"
expect_no_gh_run
verdict

run "existing binary is replaced" 0 OLD_BINARY=1
expect_installed
cmp -s "$dir/tilt-tui" "$root/pkg/tilt-tui" || problem "tilt-tui still has the old content"
verdict

[ "$fails" -eq 0 ] || { echo "$fails case(s) failed" >&2; exit 1; }
