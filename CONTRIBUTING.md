# Contributing to APiX

## Getting Started

**Prerequisites:**
- Go 1.25+
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
│   ├── breakpoints/        # Breakpoint manager
│   ├── config/             # Config loader (config.yaml)
│   ├── engine/             # Core engine orchestration
│   ├── plugins/            # Plugin runtime, SDK, and builtins
│   │   └── builtins/       # HeaderEditor, MockResponse, EnvSubst
│   ├── proxy/              # HTTP/HTTPS MITM proxy
│   ├── replay/             # Request replay logic
│   ├── server/             # gRPC server handlers
│   ├── storage/            # SQLite persistence
│   └── utils/              # Shared utilities
├── pkg/api/
│   ├── proto/apix.proto    # gRPC service definition
│   └── generated/          # Auto-generated Go protobuf code
├── apix-vscode/            # VS Code extension (TypeScript)
│   ├── src/                # Extension source
│   └── proto/              # Proto copy (synced by `make proto`)
└── tests/                  # Integration tests
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

## Adding a Plugin

1. Create a file in `internal/plugins/builtins/`, e.g. `rate_limiter.go`:

```go
package builtins

import (
    "context"
    "github.com/mnafshin/apix/internal/plugins"
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
runtime.Register(&builtins.RateLimiter{})
```

3. Add tests in `internal/plugins/builtins/builtins_test.go`.

The full `Plugin` interface is documented in [`internal/plugins/sdk.go`](internal/plugins/sdk.go).

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
