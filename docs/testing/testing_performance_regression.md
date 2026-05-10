# Performance regression thresholds

APiX keeps a small, deterministic benchmark gate for critical paths so regressions are detected before release.

## Scope

Current CI-gated benchmarks:

- `BenchmarkStorage_SaveRequest`
- `BenchmarkStorage_ListTransactions`
- `BenchmarkPluginRuntime_RunRequest_5Plugins`
- `BenchmarkHTTPProxy_Parallel`
- `BenchmarkBreakpoints_Evaluate`

Thresholds are tracked in: `scripts/perf/thresholds.tsv`

## Run locally

```bash
make bench
make perf-check
```

`make perf-check` runs the benchmark set and fails when `ns/op` or `allocs/op` exceed the configured max for any benchmark.

## Updating thresholds

Only update thresholds when:

1. A real improvement or unavoidable architecture tradeoff is intentionally introduced.
2. The benchmark was run repeatedly on comparable hardware and noise is ruled out.
3. The PR description explains why the threshold change is justified.

If performance improves, tighten thresholds in the same PR.
