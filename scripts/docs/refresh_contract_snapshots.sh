#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/contracts"
mkdir -p "$OUT_DIR"

go run ./cmd/apix-cli help 2>&1 | grep -v "Config file not found" >"$OUT_DIR/cli-help.txt"

awk '
  /^[a-zA-Z_][a-zA-Z0-9_]*:/ {
    key=$1
    sub(":", "", key)
    print key
  }
' "$ROOT_DIR/internal/config/config.yaml" | sort >"$OUT_DIR/config-keys.txt"

awk '
  /^service[[:space:]]+/ {
    print $0
  }
  /^[[:space:]]*rpc[[:space:]]+/ {
    print $0
  }
' "$ROOT_DIR/pkg/api/proto/apix.proto" >"$OUT_DIR/proto-rpcs.txt"
