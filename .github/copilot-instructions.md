# APiX – Copilot Instructions

APiX is a developer-first HTTP/HTTPS proxy and API debugging toolkit. It acts as an intercepting proxy (like mitmproxy) that captures traffic and exposes it via a gRPC server. The current tree contains the Go engine, the VS Code extension, and a first-class `cmd/apix-cli` built on the same engine API.

## Build & Run

```bash
# Build the binaries
go build ./cmd/apix-engine
go build ./cmd/apix-cli

# Run the engine (HTTP proxy :8080, gRPC :9090)
./apix-engine

# Send traffic through the proxy
curl -x http://localhost:8080 https://example.com

# Inspect gRPC services via reflection
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext localhost:9090 apix.Engine/GetStatus

# Build the VS Code extension
cd apix-vscode
npm install
npm run compile            # compile TypeScript → out/
npx tsc --noEmit           # type-check only (no output)
```

Use `make test`, `make lint`, `make build-cli`, and `make ext-build` for the current workflow. Manual proxy validation is typically done via `curl`, the CLI, and the VS Code extension.

## Full Development Setup

To run the full stack locally (engine + VS Code extension):

1. Build the engine binary at the repo root:
   ```bash
   go build -o apix-engine ./cmd/apix-engine/
   ```

2. Open the repo in VS Code and press **F5** (or run the *Run Extension* launch config in `apix-vscode/.vscode/launch.json`). The extension automatically detects an `apix-engine` binary in the workspace root.

3. The extension auto-starts the engine on activation. You can also run **APiX: Start Engine** from the Command Palette.

4. Configure your browser or `curl` to use `http://localhost:8080` as an HTTP proxy.

## Architecture

The project currently has two Go binaries, a VS Code extension, and a layered internal structure:

```
cmd/apix-engine  →  internal/server/grpc.go (gRPC, :9090)
                 →  internal/proxy/http.go  (HTTP proxy, :8080)
                          ↓
                    internal/engine         (request store, pub/sub channels,
                                             breakpoint orchestration)
                          ↓
                    internal/breakpoints    (URL pattern matching, pause/resume)
                    internal/replay         (request replay engine)
                    internal/storage        (SQLite persistence)
                    internal/plugins        (plugin runtime + built-ins)
                          ↓
                    pkg/api/generated       (Protobuf-generated gRPC types)

cmd/apix-cli     →  gRPC client connecting to :9090

apix-vscode/     →  VS Code extension
    src/engineClient.ts        — typed gRPC client (proto-loader)
    src/engineProcessManager.ts — spawns/monitors the engine binary
    src/extension.ts           — activation, commands, tree view wiring
    src/trafficProvider.ts     — Traffic tree view (calls GetHistory)
    src/breakpointsProvider.ts — Breakpoints tree view
    src/trafficPanel.ts        — Webview panel for traffic inspection
    src/requestEditor.ts       — Webview editor for paused requests
    src/replayPanel.ts         — Webview panel for request replay
    proto/apix.proto           — Copy of pkg/api/proto/apix.proto (kept in sync)
```

### End-to-End Request Flow

1. User configures browser/curl to proxy through `:8080`
2. HTTP proxy captures request → calls `engine.StoreTransaction()` (pub/sub broadcast) and optionally `engine.PauseRequest()` if a breakpoint matches
3. **Traffic view**: `CaptureTraffic` gRPC stream (→ `engineClient.captureTraffic()`) delivers `HttpRequest` messages; extension wraps each into a partial `HttpTransaction` for display
4. **Breakpoints**: `WatchPausedRequests` gRPC stream delivers `PausedRequest` messages; extension opens `RequestEditor` webview
5. User edits request → extension calls `ResumeRequest` with a `ResumeAction` (FORWARD/DROP/RESPOND) and optional `modifiedRequest`/`modifiedResponse`
6. **Replay**: user triggers `ReplayRequest` with a `ReplaySpec`; engine re-sends request and streams back `HttpResponse`
7. **History**: `GetHistory` streams `HttpTransaction` records from SQLite

## Key Packages and Their Status

| Package | Path | Status |
|---|---|---|
| Engine core | `internal/engine/` | ✅ Working |
| HTTP proxy | `internal/proxy/http.go` | ✅ Working |
| HTTPS/TLS proxy | `internal/proxy/https.go` | ✅ Working |
| gRPC server | `internal/server/grpc.go` | ✅ Working |
| Breakpoints | `internal/breakpoints/` | ✅ Working |
| Replay engine | `internal/replay/` | ✅ Working |
| Plugin runtime | `internal/plugins/` | ✅ Working (3 built-ins) |
| SQLite storage | `internal/storage/` | ✅ Working |
| Config | `internal/config/` | ✅ Working |
| VS Code extension | `apix-vscode/` | ✅ Working |
| CLI | `cmd/apix-cli/` | ✅ Working |

## Protobuf Workflow

The gRPC API is defined in `pkg/api/proto/apix.proto` — **this is the source of truth**. After editing the proto file:

1. Regenerate the Go bindings:
   ```bash
   protoc --go_out=pkg/api/generated --go-grpc_out=pkg/api/generated \
     --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
     pkg/api/proto/apix.proto
   ```

2. Copy the updated proto to the extension:
   ```bash
   cp pkg/api/proto/apix.proto apix-vscode/proto/apix.proto
   ```

The extension uses `@grpc/proto-loader` (dynamic loading) so no TypeScript code generation is needed — only the `.proto` file copy.

Do not edit files in `pkg/api/generated/` directly — they are overwritten on regeneration.

### Proto → TypeScript field name mapping

`@grpc/proto-loader` is configured with `keepCase: false`, so proto `snake_case` fields are automatically camel-cased in TypeScript (e.g., `url_pattern` → `urlPattern`, `status_code` → `statusCode`). Enum values are returned as strings (`enums: String`), matching the `ResumeActionKind` string enum in `types.ts`.

## Conventions

- **Package names**: Lowercase single nouns (`engine`, `server`, `config`, `plugins`).
- **Receiver names**: Single or two-letter abbreviations (`e *Engine`, `s *EngineServer`).
- **Error handling**: `log.Fatalf` for startup failures; `http.Error` / returned errors for runtime failures. No custom error types yet.
- **Concurrency**: Goroutines + `sync.WaitGroup` for graceful shutdown; `sync.Mutex` guards shared engine state; channels (`chan *apix.HttpRequest`) for pub/sub streaming.
- **Config**: YAML only (`internal/config/config.yaml`). Add new settings there and update the `Config` struct.
- **Logging**: Standard `log` package throughout. No structured logging yet.
- **`internal/` vs `pkg/`**: Core engine logic goes in `internal/`. Anything intended to be importable as a library (plugins SDK, storage interface) goes in `pkg/`.
- **TypeScript**: Plain interfaces in `types.ts` mirror proto message shapes. No generated TS code — use `keepCase: false` + camelCase manually. Extension host code only (no webview imports of VS Code API).
# APiX – Copilot Instructions

APiX is a developer-first HTTP/HTTPS proxy and API debugging toolkit. It acts as an intercepting proxy (like mitmproxy) that captures traffic and exposes it via a gRPC server. The repository currently centers on the engine, the VS Code extension, and the gRPC-backed CLI.

## Build & Run

```bash
# Build the binaries
go build ./cmd/apix-engine
go build ./cmd/apix-cli

# Run the engine (HTTP proxy :8080, gRPC :9090)
./apix-engine

# Send traffic through the proxy
curl -x http://localhost:8080 https://example.com

# Inspect gRPC services via reflection
grpcurl -plaintext localhost:9090 list
```

Use the repository Makefile for the current workflow. Manual proxy validation is typically done via `curl`, the CLI, and the VS Code extension.

## Architecture

The codebase currently has two Go binaries and a layered internal structure:

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
| Plugin runtime | `internal/pluginrt/` | ✅ Working |
| Storage backend | `internal/storage/` | ✅ Working |
| Tamper engine | breakpoint resume actions + replay | ✅ Working |
| WebSocket proxy | `internal/proxy/websocket.go` | ✅ Working |

Several future-facing `pkg/` namespaces remain intentionally narrow, but the core engine, storage, replay, and WebSocket proxy paths are implemented under `internal/`.

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
