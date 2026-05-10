# APiX Test Strategy

This document describes APiX's comprehensive multi-layer test coverage and how to contribute tests.

## Overview

APiX uses a layered testing approach to ensure reliability:

1. **Unit Tests** — Package-level logic in isolation
2. **Integration Tests** — Multiple components wired together
3. **E2E Tests** — Full-stack scenarios through gRPC
4. **Contract, Fuzz, and Benchmark Tests** — Specialized coverage
5. **Planned next layers** — CLI/MCP contract safety, stateful workflows, resilience, and release smoke coverage

```
Unit Tests (70+)              Integration Tests (16)          E2E Tests (6)
├─ Config loading            ├─ Proxy→Storage pipeline      ├─ Breakpoint DROP action
├─ Plugin behavior           ├─ gRPC auth over TCP           ├─ Breakpoint RESPOND
├─ Storage queries           ├─ Replay engine                ├─ Plugin body mutation
├─ Breakpoint evaluation     └─ Error paths                  ├─ Concurrent requests
├─ Replay logic              (real HTTP, gRPC, SQLite)       ├─ History filtering
└─ Engine orchestration                                      └─ Full breakpoint lifecycle

Contract Tests (2)            Fuzz Tests (18 seeds)           Benchmarks (5)
├─ Proto sync                 ├─ EnvSubst injection          ├─ Proxy throughput
└─ Generated code freshness   ├─ Header manipulation         ├─ Storage latency
                               ├─ URL pattern ReDoS           ├─ Pattern matching
                               └─ Input validation            └─ Query performance
```

## Next planned test mechanisms

The current suite covers the engine well, but upcoming CLI and MCP work adds new contract surfaces and long-lived workflows. To stay on track as those land, APiX should add the following layers:

### 1. MCP contract and transcript regression tests

**Purpose:** Protect tool schemas, response shapes, and representative agent workflows from silent drift.

**Why it matters:** MCP is an automation contract, not just another UI. A minor response-shape change can break agents even when the code still compiles.

### 2. Stateful workflow tests

**Purpose:** Exercise lifecycle transitions such as breakpoint add → hit → pause → forward/drop/respond, replay from stored history, and future rule interactions.

**Why it matters:** As APiX gains CLI, rules, and MCP clients, correctness depends on state transitions across components and clients, not only isolated handlers.

### 3. Fault-injection and resilience tests

**Purpose:** Deliberately simulate timeouts, stream disconnects, malformed upstream responses, engine restarts, and reconnect behavior.

**Why it matters:** CLI and MCP clients will depend on long-lived streams and recoverable failures in real development loops.

### 4. Release smoke tests

**Purpose:** Validate built artifacts with lightweight real-world checks across the engine, VS Code extension, and upcoming CLI.

**Why it matters:** This catches packaging, binary-layout, generated-file, and wiring problems that unit and package tests often miss.

## Test Layers

### 1. Unit Tests

**Location:** `internal/*_test.go`

**Purpose:** Verify package-level logic in isolation with mocks/stubs.

**Examples:**
- `internal/config/config_test.go` — Config loading, defaults, env var overrides
- `internal/breakpoints/breakpoints_test.go` — Pattern matching, rule management
- `internal/storage/storage_test.go` — CRUD, pagination, edge cases
- `internal/pluginrt/builtins/builtins_test.go` — Plugin behavior, secret blocking

**Run:**
```bash
go test ./internal/... -v
```

**Guidelines:**
- Mock external dependencies (upstream servers, file I/O)
- Use `:memory:` SQLite for test isolation
- Keep tests small and focused (<50 lines per test)
- Use table-driven tests for multiple scenarios

### 2. Integration Tests

**Location:** `tests/integration/*_test.go`

**Purpose:** Verify multiple components work together with real (but local) infrastructure.

**Test Suites:**
- `proxy_storage_test.go` (9 tests)
  - Real HTTP proxy on random TCP port
  - Real in-memory storage
  - Direct database queries (no gRPC)
  - Covers: request capture, status codes, concurrent writes, modifications, replay

- `grpc_auth_test.go` (7 tests)
  - Real TCP listener (not bufconn)
  - gRPC interceptor verification
  - Covers: token validation, empty-token mode, streaming calls, Bearer format

**Run:**
```bash
go test ./tests/integration/... -v
go test -run TestIntegration_ProxyStoresCorrectFields -v ./tests/integration/
```

**Guidelines:**
- Use `newProxyStack()` helper for consistent setup
- Real TCP for network tests (not in-process pipes)
- In-memory storage to avoid file I/O
- Parallel-safe tests with `t.Parallel()`
- Clean up with `defer stack.stop()`

**Example:**
```go
func TestIntegration_ProxyStoresRequest(t *testing.T) {
    t.Parallel()

    stack := newProxyStack(t)
    defer stack.stop()

    // Send request through proxy
    client := stack.client()
    resp, _ := client.Get("http://example.com/test")

    // Verify stored
    records, _, _ := stack.db.ListTransactions(10, 0, "", "", 0)
    if len(records) == 0 {
        t.Fatal("request not captured")
    }
}
```

### 3. E2E Tests

**Location:** `tests/e2e/*_test.go`

**Purpose:** Verify complete workflows from user perspective.

**Coverage:**
- Breakpoint actions (DROP, RESPOND, MODIFY)
- Plugin mutations (body rewriting)
- History retrieval and filtering
- Concurrent request handling
- Error scenarios

**Run:**
```bash
go test ./tests/e2e/... -v
```

### 4. Contract Tests

**Location:** `tests/contract/*_test.go`

**Purpose:** Verify interface contracts are maintained.

**Current Tests:**
- `proto_sync_test.go`
  - `TestContract_ProtoSymlinkSync` — Verifies proto symlink to Go source
  - `TestContract_GeneratedGoMatchesProto` — Regenerates proto, diffs output

**Run:**
```bash
go test ./tests/contract/... -v
```

**Guidelines:**
- Auto-skip when tools are unavailable (e.g., `protoc`)
- No external dependencies if possible
- Fast (<1s typically)

### 5. Fuzz Tests

**Location:** `internal/*_fuzz.go` and seed corpus

**Purpose:** Find edge cases and panic paths with adversarial input.

**Tests:**
- `FuzzEnvSubst_Body` — Environment variable substitution injection
- `FuzzEnvSubst_Headers` — Header manipulation with untrusted input
- `FuzzEvaluate` — URL pattern regex with pathological inputs

**Seed Corpus:** 18 hand-crafted cases covering:
- Malformed syntax (`{{UNCLOSED`, `$}`))
- Excessively nested patterns
- ReDoS attacks on regex (`(a+)+b`)

**Run:**
```bash
# Run seed corpus as regular unit tests
go test -run Fuzz ./internal/pluginrt/builtins ./internal/breakpoints

# Run continuous fuzzing locally (1 minute)
go test -fuzz=FuzzEnvSubst_Body -fuzztime=1m ./internal/pluginrt/builtins
```

**Guidelines:**
- Each `Fuzz*` test must handle ANY input without panicking
- Keep seed corpus small and fast (<1s total)
- Seeds run in CI without external fuzzer
- Local fuzzing can run longer for regression detection

### 6. Benchmarks

**Location:** `internal/**/*_test.go` (benchmarks are `func Benchmark...` in test files)

**Purpose:** Establish performance baselines and detect regressions.

**Benchmarks:**
- `BenchmarkHTTPProxy_Serial` — Sequential request throughput
- `BenchmarkHTTPProxy_Parallel` — Concurrent request throughput
- `BenchmarkStorage_SaveRequest` — SQLite insert rate
- `BenchmarkStorage_ListTransactions` — Query + scan with seeded rows
- `BenchmarkBreakpoints_Evaluate` — Regex breakpoint matching
- `BenchmarkPluginRuntime_RunRequest_0Plugins` — runtime baseline overhead
- `BenchmarkPluginRuntime_RunRequest_1Plugin` — single plugin overhead
- `BenchmarkPluginRuntime_RunRequest_5Plugins` — medium chain overhead
- `BenchmarkPluginRuntime_RunRequest_14Plugins` — full built-in chain overhead

**Run:**
```bash
go test -bench=. -benchmem ./internal/proxy ./internal/storage ./internal/breakpoints ./internal/pluginrt

# Run one benchmark
go test -bench=BenchmarkStorage_SaveRequest -benchmem ./internal/storage

# CI regression gate with explicit thresholds
make perf-check
```

**Guidelines:**
- Use real components (proxy, storage, engine) not mocks
- Reset timer after setup: `b.ResetTimer()`
- Allocations matter: `-benchmem` shows allocs/op
- Thresholds are maintained in `scripts/perf/thresholds.tsv`
- Any threshold violation is a release-blocking regression

## Infrastructure & Helpers

### `newProxyStack(t *testing.T) *proxyStack`

**Location:** `tests/integration/proxy_storage_test.go`

**Returns:**
- Real HTTP proxy on random TCP port
- In-memory SQLite storage
- Engine with breakpoints + plugin runtime
- Helper methods: `client()`, `stop()`

**Use for:** Integration tests that need full proxy stack

```go
stack := newProxyStack(t)
defer stack.stop()

client := stack.client()
resp, _ := client.Get("http://example.com/test")

records, _, _ := stack.db.ListTransactions(10, 0, "", "", 0)
```

### `newGRPCStackWithAuth(t *testing.T, token string) *grpcAuthStack`

**Location:** `tests/integration/grpc_auth_test.go`

**Returns:**
- Real TCP listener with gRPC server
- Auth token configured
- Client with metadata setup
- Helper: `ctxWithToken(token)` for metadata

**Use for:** gRPC integration tests over real TCP

```go
stack := newGRPCStackWithAuth(t, "secret")
defer stack.close()

ctx := stack.ctxWithToken("secret")
status, err := stack.client.GetStatus(ctx, &Empty{})
```

### `:memory:` SQLite

**Why:** Faster than disk I/O, automatic cleanup, thread-safe for concurrent tests

**Gotcha:** With default `MaxOpenConns(25)`, each concurrent goroutine gets a separate empty in-memory DB. **Fixed** by capping `MaxOpenConns(1)` + `MaxIdleConns(1)` for `:memory:`.

**Usage:**
```go
db, _ := storage.Open(":memory:")
defer db.Close()
// Use db in tests
```

## Test Quality Checklist

When adding a test:

- [ ] **Focused** — Tests one thing (one assertion if possible)
- [ ] **Independent** — No global state, no test dependencies, parallel-safe (`t.Parallel()`)
- [ ] **Automatic cleanup** — Uses `t.Cleanup()`, `t.TempDir()`, `defer cleanup()`
- [ ] **Clear name** — `TestFunctionality_Scenario_ExpectedOutcome`
- [ ] **Real components** (integration) — Not mocked, but local (random ports)
- [ ] **Fast** — <100ms per test; <1s total per package
- [ ] **Deterministic** — No timeouts, no sleeps (unless necessary), no randomness without seeding
- [ ] **Documented** — Comments explain setup, assertions, and why this test matters

## CI/CD

### GitHub Actions Workflow

File: `.github/workflows/ci.yml`

```yaml
go build ./...           # Compiles all packages
go test ./... -race      # Unit + integration + E2E with race detector
tsc --noEmit            # TypeScript type checks
```

**Race Detector:** Catches concurrent access bugs. All tests must pass with `-race`.

**Benchmarks:** Run on PR but not enforced (for tracking only).

**Pro Tips:**
- Always run `go test ./... -race` locally before pushing
- Integration tests may be slow (~2-3s) — OK for CI
- Use `testing.Short()` to skip slow tests in quick CI runs

## Adding a New Test

### 1. Decide the Layer

| Layer | When to use |
|-------|-------------|
| Unit | Testing a single package in isolation |
| Integration | Testing 2+ components together (proxy, storage, gRPC) |
| E2E | Testing complete user workflow |
| Contract | Testing API/interface stability |
| Fuzz | Testing edge cases and panic paths |
| Benchmark | Tracking performance |

### 2. Create Test File

```go
package mypackage_test

import (
    "testing"
    "github.com/mnafshin/apix/internal/mypackage"
)

func TestMyFeature_HappyPath(t *testing.T) {
    t.Parallel()

    // Setup
    obj := mypackage.New()

    // Execute
    result := obj.Do()

    // Assert
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### 3. Run Locally

```bash
go test ./internal/mypackage -v
go test ./internal/mypackage -race
```

### 4. Commit

```bash
git add internal/mypackage/myfeature_test.go
git commit -m "test: add MyFeature tests"
```

## Debugging Tests

### Run with Verbose Output
```bash
go test ./internal/package -v
```

### Run with Printf Debugging
```bash
go test ./internal/package -v -run TestName -args -test.v
```

### Debug with Delve (debugger)
```bash
dlv test ./internal/package -- -test.run TestName
```

### Check for Race Conditions
```bash
go test ./... -race
```

### Coverage Report
```bash
make test-coverage
open coverage.html
```

## Performance Baselines

Use these as reference points when adding performance-sensitive tests:

| Component | Operation | Baseline | Acceptable Range |
|-----------|-----------|----------|------------------|
| HTTP Proxy | Request (serial) | ~66µs | <100µs |
| HTTP Proxy | Request (parallel) | ~42µs | <60µs |
| Storage | Write (SaveRequest) | ~12µs | <20µs |
| Storage | Query (ListTransactions w/ 1000 rows) | ~1ms | <2ms |
| Breakpoints | Evaluate (10 rules) | ~7µs | <15µs |

If benchmarks regress >10%, investigate the cause:
```bash
go test -bench=BenchmarkName -benchmem -count=5 ./package
```

## Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Golang Best Practices](https://golang.org/doc/effective_go)
- [Table-Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [Go Fuzzing Guide](https://go.dev/doc/fuzz/)
- [Go Benchmarking Guide](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
