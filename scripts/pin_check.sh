#!/usr/bin/env bash
# Fail when a GitHub Actions `uses:` line isn't pinned to a full commit SHA.
# Usage: scripts/pin_check.sh [dir]   (default .github/workflows)
set -euo pipefail

dir="${1:-.github/workflows}"
[ -d "$dir" ] || exit 0

bad=0
while IFS= read -r line; do
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
done < <(grep -rnE '^[[:space:]]*(-[[:space:]]*)?uses:' "$dir" --include='*.yml' --include='*.yaml' || true)

exit "$bad"
