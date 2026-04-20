#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TAG="${1:-${GITHUB_REF_NAME:-}}"
OUT="${2:-$ROOT_DIR/release-notes.md}"

if [[ -z "$TAG" ]]; then
  echo "usage: $0 <tag> [output-file]" >&2
  exit 1
fi

{
  echo "## APiX ${TAG}"
  echo
  echo "### Upgrade and compatibility"
  echo "- [Changelog](CHANGELOG.md)"
  echo "- [Upgrade Guide](UPGRADE.md)"
  echo "- [Compatibility Matrix](docs/REFERENCE/compatibility-matrix.md)"
  echo "- [Stability Policy](docs/REFERENCE/stability-policy.md)"
  echo
  echo "### Changelog section"
  if ! awk -v tag="$TAG" '
    $0 ~ "^## \\[" tag "\\]" {print; found=1; next}
    found && /^## \[/ {exit}
    found {print}
    END { if (!found) exit 1 }
  ' "$ROOT_DIR/CHANGELOG.md"; then
    echo "- No dedicated changelog section found for ${TAG}. See [CHANGELOG.md](CHANGELOG.md)."
  fi
} >"$OUT"
