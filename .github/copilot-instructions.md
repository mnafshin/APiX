# APiX – Copilot Instructions

APiX is a developer-first HTTP/HTTPS proxy and API debugging toolkit. It acts as an intercepting proxy (like mitmproxy) that captures traffic and exposes it via a gRPC server. A CLI (`apix-cli`) connects to the engine to stream captured traffic, query status, and list plugins.

## Build & Run

```bash
# Build both binaries
go build ./cmd/apix-engine
go build ./cmd/apix-cli

# Run the engine (HTTP proxy :8080, gRPC :9090)
./apix-engine

# Run CLI commands
./apix-cli status
./apix-cli log
./apix-cli plugins

# Send traffic through the proxy
curl -x http://localhost:8080 https://example.com

# Inspect gRPC services via reflection
grpcurl -plaintext localhost:9090 list
```

There are currently no tests, Makefile, or CI configs. Manual testing is done via `curl` and the CLI.

## Architecture

The codebase has two binaries and a layered internal structure:

```
cmd/apix-engine  →  internal/server (HTTP proxy + gRPC server)
                          ↓
                    internal/engine  (request store, pub/sub via channels)
                          ↓
                    pkg/api/generated  (Protobuf-generated gRPC types)

cmd/apix-cli     →  gRPC client connecting to :9090
```

- **`internal/engine`** – Core: thread-safe request storage (`sync.Mutex` over a slice) and a subscriber channel map for streaming traffic to gRPC clients.
- **`internal/server/http.go`** – HTTP reverse proxy that captures every request into the engine.
- **`internal/server/grpc.go`** – gRPC server implementing `EngineServer`; streams captured requests to CLI subscribers.
- **`internal/config`** – Loads `config.yaml` (HTTP port, gRPC port). Default: `8080` / `9090`.
- **`pkg/api/proto/apix.proto`** – Source of truth for the gRPC API. Generated code lives in `pkg/api/generated/`.

## Key Packages and Their Status

| Package | Path | Status |
|---|---|---|
| Engine core | `internal/engine/` | ✅ Working |
| HTTP proxy | `internal/server/http.go` | ✅ Working |
| gRPC server | `internal/server/grpc.go` | ✅ Working |
| Config | `internal/config/` | ✅ Working |
| Plugin runtime | `pkg/plugins/` | 🔄 Empty stub |
| Storage backend | `pkg/storage/` | 🔄 Empty stub |
| Tamper engine | `pkg/tamper/` | 🔄 Empty stub |
| WebSocket proxy | `pkg/proxy/websocket/` | 🔄 Empty stub |

Most `pkg/` packages are intentional stubs for planned features (v0.2+).

## Protobuf Workflow

The gRPC API is defined in `pkg/api/proto/apix.proto`. After editing the proto file, regenerate the Go code:

```bash
protoc --go_out=pkg/api/generated --go-grpc_out=pkg/api/generated \
  --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
  pkg/api/proto/apix.proto
```

Do not edit files in `pkg/api/generated/` directly — they are overwritten on regeneration.

## Conventions

- **Package names**: Lowercase single nouns (`engine`, `server`, `config`, `plugins`).
- **Receiver names**: Single or two-letter abbreviations (`e *Engine`, `s *EngineServer`).
- **Error handling**: `log.Fatalf` for startup failures; `http.Error` / returned errors for runtime failures. No custom error types yet.
- **Concurrency**: Goroutines + `sync.WaitGroup` for graceful shutdown; `sync.Mutex` guards shared engine state; channels (`chan *apix.HttpRequest`) for pub/sub streaming.
- **Config**: YAML only (`internal/config/config.yaml`). Add new settings there and update the `Config` struct.
- **Logging**: Standard `log` package throughout. No structured logging yet.
- **`internal/` vs `pkg/`**: Core engine logic goes in `internal/`. Anything intended to be importable as a library (plugins SDK, storage interface) goes in `pkg/`.
