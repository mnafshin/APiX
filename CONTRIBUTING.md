# Contributing to APiX

## Getting Started

**Prerequisites:**
- Go 1.24+
- Node.js 20+
- `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` (for proto changes only)

```bash
git clone https://github.com/mnafshin/apix.git
cd apix
make dev        # builds engine + VS Code extension
make test       # run Go tests
```

## Project Structure

```
apix/
├── cmd/apix-engine/        # Engine entry point (main.go)
├── internal/
│   ├── breakpoints/        # Breakpoint manager + tests
│   ├── config/             # Config loader (config.yaml)
│   ├── engine/             # Core engine orchestration
│   ├── pluginrt/           # Plugin runtime + builtins (HeaderEditor, MockResponse, EnvSubst)
│   ├── proxy/              # HTTP/HTTPS MITM proxy + benchmarks
│   ├── replay/             # Request replay logic
│   ├── server/             # gRPC server handlers
│   ├── storage/            # SQLite persistence + benchmarks
│   └── utils/              # Shared utilities
├── pkg/api/
│   ├── proto/apix.proto    # gRPC service definition
│   └── generated/          # Auto-generated Go protobuf code
├── apix-vscode/            # VS Code extension (TypeScript)
│   ├── src/                # Extension source
│   └── proto/              # Proto copy (synced by `make proto`)
├── tests/
│   ├── contract/           # Contract tests (proto sync verification)
│   ├── integration/        # Integration tests (proxy→storage, gRPC auth, replay)
│   └── e2e/                # End-to-end tests
└── docs/                   # Documentation
```

## Development Workflow

| Command | Description |
|---------|-------------|
| `make build` | Build the engine binary |
| `make test` | Run all Go tests with race detector |
| `make test-one TEST=<name> PKG=<path>` | Run a single named test |
| `make test-coverage` | Tests with HTML coverage report |
| `make lint` | Run `go vet` |
| `make proto` | Regenerate Go + copy proto to extension |
| `make ext-build` | Compile the VS Code extension |
| `make ext-package` | Package extension as `.vsix` |
| `make build-all` | Cross-compile engine for all platforms |
| `make dev` | Build engine + extension |
| `make clean` | Remove build artifacts |

## Testing

APiX has comprehensive multi-layer test coverage:

### Test Organization

| Layer | Location | Purpose | Examples |
|-------|----------|---------|----------|
| **Unit Tests** | `internal/*_test.go` | Package-level logic | Config loading, plugin behavior, storage queries |
| **Integration Tests** | `tests/integration/*_test.go` | Multiple components | Proxy→Storage pipeline, gRPC auth over TCP, Replay engine |
| **E2E Tests** | `tests/e2e/*_test.go` | Full-stack scenarios | Breakpoint actions, concurrent requests, history filtering |
| **Contract Tests** | `tests/contract/*_test.go` | Interface agreements | Proto file sync, generated code freshness |
| **Fuzz Tests** | `internal/*_fuzz.go` | Input validation | EnvSubst injection, URL pattern ReDoS |
| **Benchmarks** | `internal/*_bench.go` | Performance baselines | Request throughput, query latency, pattern matching |

### Running Tests

```bash
# All tests (with race detector)
make test

# Integration tests only
go test ./tests/integration/... -v

# Run benchmarks
go test -bench=. -benchmem ./internal/proxy ./internal/storage ./internal/breakpoints

# Run with coverage
make test-coverage

# Run fuzz tests with seeds
go test -run Fuzz ./internal/pluginrt/builtins ./internal/breakpoints
```

### Writing Tests

**Conventions:**
- Use `t.Helper()` to improve error messages
- Use `t.Parallel()` for safe tests
- Use `:memory:` SQLite for test isolation
- Table-driven tests for multiple scenarios
- Auto-cleanup with `t.Cleanup()` or `t.TempDir()`

**Example: Integration test**
```go
func TestIntegration_ProxyStoresRequest(t *testing.T) {
    t.Parallel()

    // Setup: real proxy + engine + in-memory storage
    db, _ := storage.Open(":memory:")
    defer db.Close()

    // Mock upstream
    upstream := httptest.NewServer(http.HandlerFunc(...))
    defer upstream.Close()

    // Send request through proxy
    client := &http.Client{...}
    resp, _ := client.Get(upstream.URL + "/test")

    // Verify stored in DB
    records, _, _ := db.ListTransactions(10, 0, "", "", 0)
    // assert records[0].URL contains "/test"
}
```

### CI Requirements

All PRs must pass:
- ✅ `go build ./...` — compiles
- ✅ `go test ./... -race` — all tests pass, no race conditions
- ✅ `go vet ./...` — no suspicious constructs
- ✅ `tsc --noEmit` — TypeScript types valid
- ✅ `scripts/docs/verify_contract_snapshots.sh` — CLI/config/proto docs contracts are current

1. Create a file in `internal/pluginrt/builtins/`, e.g. `rate_limiter.go`:

```go
package builtins

import (
    "context"
    "github.com/mnafshin/apix/pkg/plugins"
)

type RateLimiter struct{}

func (r *RateLimiter) Name() string        { return "rate-limiter" }
func (r *RateLimiter) Version() string     { return "1.0.0" }
func (r *RateLimiter) Description() string { return "Throttles outbound requests" }

func (r *RateLimiter) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
    // modify or block the request
    return nil, nil // nil = pass through unchanged
}

func (r *RateLimiter) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
    return nil, nil
}
```

2. Register it in `cmd/apix-engine/main.go` alongside the existing plugins:

```go
pluginRT.Register(&builtins.RateLimiter{})
```

3. Add tests in `internal/pluginrt/builtins/builtins_test.go`.

The full `Plugin` interface is documented in [`pkg/plugins/sdk.go`](pkg/plugins/sdk.go).

## Proto Changes

1. Edit [`pkg/api/proto/apix.proto`](pkg/api/proto/apix.proto)
2. Run `make proto` — this regenerates `pkg/api/generated/` and copies the proto to `apix-vscode/proto/`
3. Update TypeScript types in `apix-vscode/src/types.ts` to match any new messages or fields
4. Update server handler in `internal/server/` for new RPCs

## Code Style

**Go:**
- Format with `gofmt` (enforced in CI via `go vet`)
- Short, single-letter receiver names (`s *Server`, `r *RateLimiter`)
- Use the standard `log` package — no third-party loggers
- Error strings lowercase, no trailing punctuation

**TypeScript:**
- Strict mode is on (`"strict": true` in `tsconfig.json`)
- Follow VS Code extension conventions — use `vscode.window.*` for user-facing messages
- Avoid `any` — use generated proto types from `types.ts`

## Pull Requests

- CI must pass: `go build ./...`, `go test ./internal/... -race`, `tsc --noEmit`
- Add or update tests for new features
- Keep PRs focused — one logical change per PR

## Documentation Status Checklist

When your PR touches docs (or behavior docs rely on), verify:

- Runtime behavior claims match current code paths and defaults
- Config examples use keys that exist in `internal/config/config.go`
- CLI examples match current commands/flags from `./apix-cli help`
- Performance numbers are marked as measured only when benchmark-backed

Refresh snapshots when user-facing contracts change:

```bash
scripts/docs/refresh_contract_snapshots.sh
```
