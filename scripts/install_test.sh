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

# Restricted tool dir. It holds no gh and no uname, and its curl is fake.
mkdir -p "$root/tools" "$root/fakebin" "$root/ghbin"
real_curl="$(command -v curl)" || { echo "install_test: missing curl" >&2; exit 1; }
for tool in bash tar gzip awk grep mktemp rm mkdir cp chmod mv tr tail cat head dirname env "$sha_tool"; do
  path="$(command -v "$tool")" || { echo "install_test: missing $tool" >&2; exit 1; }
  ln -s "$path" "$root/tools/$tool"
done

# The fake curl answers a -I call for <base>/releases/latest with a redirect
# to ${FAKE_LATEST_TAG:-v0.1.0}, so no case touches the network. Every
# other call goes to the real curl.
cat > "$root/tools/curl" <<EOF
#!/bin/sh
head=0
latest=0
for arg in "\$@"; do
  case "\$arg" in
    -[!-]*I*) head=1 ;;
    */releases/latest) latest=1 ;;
  esac
done
if [ "\$head" = 1 ] && [ "\$latest" = 1 ]; then
  printf 'HTTP/1.1 302 Found\r\n'
  printf 'location: https://example.invalid/SentioLabs/go-tilt-tui/releases/tag/%s\r\n' "\${FAKE_LATEST_TAG:-v0.1.0}"
  printf '\r\n'
  exit 0
fi
exec "$real_curl" "\$@"
EOF
chmod 0755 "$root/tools/curl"

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
# install dir first, and DIR_TARGET=1 puts a tilt-tui directory there.
# SLASH_DIR=1 adds a trailing slash to the install dir and puts the dir
# on PATH. LATEST=1 leaves TILT_TUI_VERSION unset. NO_HOME=1 leaves both
# HOME and TILT_TUI_INSTALL_DIR unset. It sets name, dir, rc, out and err
# for the checks below, and verdict reports the case.
problems=""
run() {
  local vars install_dir on_path latest no_home
  name="$1"; want="$2"; shift 2
  dir="$(mktemp -d "$root/case.XXXXXX")/bin"
  out="$dir.out"; err="$dir.err"
  path="$root/fakebin:$root/ghbin:$root/tools"
  install_dir="$dir"
  on_path=0; latest=0; no_home=0
  problems=""
  rm -f "$root/gh.args"
  for kv in "$@"; do
    case "$kv" in
      NO_GH=1) path="$root/fakebin:$root/tools" ;;
      OLD_BINARY=1) mkdir -p "$dir"; echo old > "$dir/tilt-tui" ;;
      DIR_TARGET=1) mkdir -p "$dir/tilt-tui" ;;
      SLASH_DIR=1) install_dir="$dir/"; on_path=1 ;;
      LATEST=1) latest=1 ;;
      NO_HOME=1) no_home=1 ;;
    esac
  done
  if [ "$on_path" = 1 ]; then
    path="$dir:$path"
  fi
  vars=("PATH=$path" "TILT_TUI_BASE_URL=file://$root/release")
  if [ "$no_home" != 1 ]; then
    vars+=("HOME=$root/home" "TILT_TUI_INSTALL_DIR=$install_dir")
  fi
  if [ "$latest" != 1 ]; then
    vars+=("TILT_TUI_VERSION=$tag")
  fi
  rc=0
  env -i "${vars[@]}" "$@" \
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
# expect_not_in <file> <text> checks that the file lacks the fixed string.
expect_not_in() {
  ! grep -qsF -- "$2" "$1" || problem "$(basename "$1") has: $2"
}
expect_no_gh_run() {
  [ ! -e "$root/gh.args" ] || problem "gh ran: $(cat "$root/gh.args")"
}

verdict() {
  local leftover
  if [ -d "$dir" ]; then
    leftover="$(find "$dir" -name '.tilt-tui.*')"
    [ -z "$leftover" ] || problem "temp file left: $leftover"
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

run "install target is a directory" 1 DIR_TARGET=1
expect_in "$err" "$dir/tilt-tui is a directory. Remove it or set TILT_TUI_INSTALL_DIR."
[ -z "$(ls -A "$dir/tilt-tui")" ] || problem "$dir/tilt-tui is not empty: $(ls -A "$dir/tilt-tui")"
verdict

run "install dir with a trailing slash on PATH" 0 SLASH_DIR=1
expect_installed
expect_not_in "$err" "is not on PATH"
expect_not_in "$err" "tilt-tui on PATH is"
verdict

run "HOME unset with no install dir" 1 NO_HOME=1
expect_in "$err" "install.sh: HOME is not set. Set TILT_TUI_INSTALL_DIR."
[ ! -e "$dir" ] || problem "$dir exists"
expect_no_gh_run
verdict

run "latest release" 0 LATEST=1
expect_installed
expect_in "$out" "tilt-tui v0.1.0"
expect_in "$root/gh.args" "attestation verify"
verdict

run "latest release with a path in its tag" 1 LATEST=1 FAKE_LATEST_TAG='v0.1.0/../../x'
expect_not_installed
expect_in "$err" "bad release tag"
expect_no_gh_run
verdict

[ "$fails" -eq 0 ] || { echo "$fails case(s) failed" >&2; exit 1; }
