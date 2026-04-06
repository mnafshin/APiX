#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="$ROOT/build"
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/apix-engine.log"
ENGINE_BIN="$ROOT/apix-engine"
E2E_TIMEOUT=30

echo "[E2E] Root: $ROOT"

echo "[E2E] 1) Run unit tests"
cd "$ROOT"
make test

echo "[E2E] 2) Build engine"
make build

if [ ! -x "$ENGINE_BIN" ]; then
  echo "[E2E] Engine binary not found or not executable: $ENGINE_BIN"
  exit 1
fi

echo "[E2E] 3) Start engine in background (logs -> $LOG)"
rm -f "$LOG"
"$ENGINE_BIN" >"$LOG" 2>&1 &
ENGINE_PID=$!

echo "[E2E] Engine PID=$ENGINE_PID"

# Ensure engine is killed on exit
cleanup() {
  echo "[E2E] Stopping engine PID=$ENGINE_PID"
  kill "$ENGINE_PID" 2>/dev/null || true
  wait "$ENGINE_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for gRPC port to be available or until timeout
echo "[E2E] Waiting up to ${E2E_TIMEOUT}s for engine gRPC (localhost:9090)"
start_ts=$(date +%s)
while true; do
  if command -v grpcurl >/dev/null 2>&1; then
    if grpcurl -plaintext localhost:9090 list >/dev/null 2>&1; then
      break
    fi
  else
    # try TCP connect via /dev/tcp
    if (echo > /dev/tcp/localhost/9090) >/dev/null 2>&1; then
      break
    fi
  fi
  if [ $(( $(date +%s) - start_ts )) -gt $E2E_TIMEOUT ]; then
    echo "[E2E] Engine did not become ready in ${E2E_TIMEOUT}s. Last logs:"
    tail -200 "$LOG"
    exit 2
  fi
  sleep 1
done

echo "[E2E] Engine appears ready. Showing brief log tail:"
tail -50 "$LOG" || true

if command -v grpcurl >/dev/null 2>&1; then
  echo "[E2E] gRPC services:"
  grpcurl -plaintext localhost:9090 list || true
  echo "[E2E] GetStatus:"
  grpcurl -plaintext localhost:9090 apix.Engine/GetStatus || true
else
  echo "[E2E] grpcurl not found; skipping gRPC interaction checks"
fi

echo "[E2E] 4) Build VS Code extension (TypeScript)"
cd "$ROOT/apix-vscode"
if [ -f package-lock.json ] || [ -f package.json ]; then
  npm ci --no-audit --no-fund
else
  npm install --no-audit --no-fund
fi
npm run compile
npx tsc --noEmit

echo "[E2E] 5) End-to-end HTTP proxy test (via curl)"
# Try a simple GET through the proxy. Accept 200, 301, 302 as success.
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -x http://localhost:8080 http://example.com || true)
echo "[E2E] HTTP status: $HTTP_STATUS"
if [ "$HTTP_STATUS" != "200" ] && [ "$HTTP_STATUS" != "301" ] && [ "$HTTP_STATUS" != "302" ]; then
  echo "[E2E] Unexpected HTTP status: $HTTP_STATUS"
  echo "[E2E] Engine logs (last 200 lines):"
  tail -200 "$LOG"
  exit 3
fi

echo "[E2E] All checks passed. Cleaning up and exiting."
# cleanup trap will stop engine
exit 0
