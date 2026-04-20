#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

"$ROOT_DIR/scripts/docs/refresh_contract_snapshots.sh"

git -C "$ROOT_DIR" diff --exit-code -- \
  docs/contracts/cli-help.txt \
  docs/contracts/config-keys.txt \
  docs/contracts/proto-rpcs.txt
