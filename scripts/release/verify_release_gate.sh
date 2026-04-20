#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DOC="$ROOT_DIR/docs/RELEASE_GATE.md"

test -f "$DOC"

grep -q "## Validation matrix" "$DOC"
grep -q "## Blocking gate" "$DOC"
grep -q "## Pre-tag checklist" "$DOC"
grep -q "## Post-release checklist" "$DOC"

