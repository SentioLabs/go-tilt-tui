#!/usr/bin/env bash
# Fail when a GitHub Actions `uses:` line isn't pinned to a full commit SHA.
# Usage: scripts/pin_check.sh [dir]   (default .github/workflows)
set -euo pipefail

dir="${1:-.github/workflows}"
[ -d "$dir" ] || exit 0

# grep exits 1 when nothing matches and above 1 on an error such as an
# unreadable file. Fail closed on errors.
rc=0
matches="$(grep -rnE '^[[:space:]]*(-[[:space:]]*)?uses:' "$dir" --include='*.yml' --include='*.yaml')" || rc=$?
if [ "$rc" -gt 1 ]; then
  echo "pin_check: grep failed with exit $rc" >&2
  exit "$rc"
fi

bad=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  ref="${line#*uses:}"
  ref="${ref#"${ref%%[![:space:]]*}"}"
  ref="${ref%%[[:space:]]*}"
  ref="${ref%\"}"; ref="${ref#\"}"; ref="${ref%\'}"; ref="${ref#\'}"
  case "$ref" in
    ./*) continue ;;
  esac
  if [[ ! "$ref" =~ @[0-9a-f]{40}$ ]]; then
    echo "not pinned to a commit SHA: $line" >&2
    bad=1
  fi
done <<< "$matches"

exit "$bad"
