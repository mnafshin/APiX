#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
THRESHOLDS="$ROOT_DIR/scripts/perf/thresholds.tsv"

if [[ ! -f "$THRESHOLDS" ]]; then
  echo "missing thresholds file: $THRESHOLDS" >&2
  exit 1
fi

TMP_RAW="$(mktemp)"
TMP_PARSED="$(mktemp)"
trap 'rm -f "$TMP_RAW" "$TMP_PARSED"' EXIT

cd "$ROOT_DIR"

go test ./internal/storage ./internal/pluginrt ./internal/proxy ./internal/breakpoints \
  -run '^$' \
  -bench 'BenchmarkStorage_SaveRequest|BenchmarkStorage_ListTransactions|BenchmarkPluginRuntime_RunRequest_5Plugins|BenchmarkHTTPProxy_Parallel|BenchmarkBreakpoints_Evaluate' \
  -benchmem -count=1 | tee "$TMP_RAW"

awk '
  /^Benchmark/ {
    name=$1
    sub(/-[0-9]+$/, "", name)
    ns=""
    allocs=""
    for (i=1; i<=NF; i++) {
      if ($i=="ns/op" && i>1) { ns=$(i-1) }
      if ($i=="allocs/op" && i>1) { allocs=$(i-1) }
    }
    if (ns!="") {
      print name "\t" ns "\t" allocs
    }
  }
' "$TMP_RAW" > "$TMP_PARSED"

echo
echo "Benchmark threshold checks:"

status=0
while IFS=$'\t' read -r name max_ns max_allocs; do
  [[ -z "${name:-}" || "${name:0:1}" == "#" ]] && continue

  row="$(awk -F '\t' -v target="$name" '$1==target {print $0}' "$TMP_PARSED" | tail -n 1)"
  if [[ -z "$row" ]]; then
    echo "FAIL: $name missing from benchmark output"
    status=1
    continue
  fi

  measured_ns="$(echo "$row" | awk -F '\t' '{print $2}')"
  measured_allocs="$(echo "$row" | awk -F '\t' '{print $3}')"
  ns_ok=0
  alloc_ok=0
  awk "BEGIN {exit !($measured_ns <= $max_ns)}" && ns_ok=1 || true
  awk "BEGIN {exit !($measured_allocs <= $max_allocs)}" && alloc_ok=1 || true

  ns_label="PASS"
  alloc_label="PASS"
  if [[ $ns_ok -ne 1 ]]; then
    ns_label="FAIL"
    status=1
  fi
  if [[ $alloc_ok -ne 1 ]]; then
    alloc_label="FAIL"
    status=1
  fi

  echo "$name  ns/op=$measured_ns (max=$max_ns, $ns_label)  allocs/op=$measured_allocs (max=$max_allocs, $alloc_label)"
done < "$THRESHOLDS"

if [[ $status -ne 0 ]]; then
  echo
  echo "performance regression threshold check failed" >&2
  exit 1
fi
