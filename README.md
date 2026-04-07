![APiX](/public/assets/img/APiX.png)

# APiX — API Debugger

> Intercept, inspect, and debug HTTP/HTTPS traffic directly in VS Code.

![Build](https://github.com/mnafshin/apix/actions/workflows/ci.yml/badge.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)
![VS Code](https://img.shields.io/badge/vscode-%5E1.85-007ACC.svg)

## What is APiX?

APiX is an API debugging toolkit that runs as a VS Code extension backed by a Go proxy engine. It intercepts HTTP/HTTPS traffic via a MITM proxy, lets you pause requests at URL breakpoints, edit and replay them, and extend behaviour with plugins — all without leaving your editor.

## Features

- 🔌 **HTTP/HTTPS intercepting proxy** with MITM support
- 🛑 **URL breakpoints** — pause, inspect, edit, and resume requests
- 🔁 **Request replay** with header/body overrides
- 🧩 **Plugin system** — HeaderEditor, MockResponse, EnvSubst (and custom)
- 💾 **SQLite persistent storage** — traffic history survives restarts
- 🖥️ **VS Code extension** — traffic inspector and breakpoints view in the sidebar
- 🌐 **Works in browser** (vscode.dev) via remote engine over TLS
- 📦 **Cross-platform** — macOS, Linux, Windows

## Quick Start

```bash
# 1. Build the engine
make build

# 2. Build the VS Code extension
make ext-build

# 3. Package and install the extension
make ext-package
make ext-install

# 4. Open VS Code — APiX starts automatically
```

Point your HTTP client at `http://localhost:8080` to route traffic through APiX.

## Architecture

```
┌─────────────────────────┐         ┌──────────────────────────────┐
│    VS Code Extension    │  gRPC   │         Go Engine            │
│  ┌───────────────────┐  │ ──────► │  ┌────────────────────────┐  │
│  │  Traffic Inspector│  │         │  │   gRPC Server (:9090)  │  │
│  │  Breakpoints View │  │ ◄────── │  │   Breakpoint Manager   │  │
│  │  Replay Panel     │  │         │  │   Plugin Runtime       │  │
│  └───────────────────┘  │         │  │   SQLite Storage       │  │
└─────────────────────────┘         │  └────────┬───────────────┘  │
                                    │           │                   │
                                    │  ┌────────▼───────────────┐  │
                                    │  │  HTTP/HTTPS Proxy      │  │
                                    │  │  (:8080, MITM)         │  │
                                    │  └────────────────────────┘  │
                                    └──────────────────────────────┘
```

## Development

```bash
# Build everything (engine + extension)
make dev

# Run Go tests (with race detector)
make test

# Run a single test
make test-one TEST=TestSaveAndGetRequest PKG=./internal/storage/

# Run tests with coverage
make test-coverage

# Lint
make lint

# Regenerate protobuf code
make proto

# Cross-compile engine for all platforms
make build-all
```

## gRPC API

The engine exposes a single `Engine` service on port `9090`.

| RPC | Type | Description |
|-----|------|-------------|
| `GetStatus` | Unary | Engine health, version, ports |
| `CaptureTraffic` | Server-stream | Live stream of captured HTTP requests |
| `ListPlugins` | Unary | List loaded plugins and their status |
| `SetBreakpoint` | Unary | Register a URL pattern to pause on |
| `DeleteBreakpoint` | Unary | Remove a breakpoint by ID |
| `ListBreakpoints` | Unary | List all active breakpoints |
| `WatchPausedRequests` | Server-stream | Stream requests that hit a breakpoint |
| `ResumeRequest` | Unary | Forward, drop, or synthetically respond to a paused request |
| `ReplayRequest` | Unary | Re-send a stored or arbitrary request with optional overrides |
| `GetHistory` | Server-stream | Query stored request/response pairs from SQLite |
| `ClearHistory` | Unary | Delete all stored traffic history |

Proto definition: [`pkg/api/proto/apix.proto`](pkg/api/proto/apix.proto)

`SetBreakpoint` validates its input: `url_pattern` is required and must not exceed 500 characters; `methods` must be standard HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, `CONNECT`, `TRACE`). Invalid input returns `codes.InvalidArgument`.

## Plugins

Built-in plugins live in `internal/plugins/builtins/`:

| Plugin | Description |
|--------|-------------|
| `HeaderEditor` | Add, modify, or remove request/response headers |
| `MockResponse` | Return a synthetic response without hitting upstream |
| `EnvSubst` | Replace `${VAR}` placeholders in request bodies with environment values |

**Custom plugins** implement the `Plugin` interface in `internal/plugins/sdk.go`:

```go
type Plugin interface {
    Name() string
    Version() string
    Description() string
    OnRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error)
    OnResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error)
}
```

Create a file in `internal/plugins/builtins/` and register it in `cmd/apix-engine/main.go`. See [CONTRIBUTING.md](CONTRIBUTING.md) for a step-by-step guide.

## Configuration

All settings live in `internal/config/config.yaml`. The file is optional — defaults are used when it is absent.

| Key | Default | Description |
|-----|---------|-------------|
| `http_port` | `8080` | HTTP proxy listen port |
| `grpc_port` | `9090` | gRPC API listen port |
| `db_path` | `apix.db` | SQLite database path |
| `ca_cert_path` | `~/.apix/ca.pem` | MITM CA certificate |
| `ca_key_path` | `~/.apix/ca-key.pem` | MITM CA private key |
| `tls_enabled` | `false` | Enable TLS on the gRPC server |
| `auth_token` | `""` | Bearer token for gRPC auth (prefer `APIX_AUTH_TOKEN` env var) |
| `max_idle_conns_per_host` | `10` | Max idle upstream connections per host |
| `idle_conn_timeout_sec` | `90` | Seconds an idle upstream connection stays open |
| `dial_timeout_sec` | `10` | Seconds allowed for a TCP dial to upstream |

## Authentication

When running with an auth token, prefer the `APIX_AUTH_TOKEN` environment variable over `auth_token` in `config.yaml` to avoid storing secrets in plaintext files.

```bash
# Recommended
APIX_AUTH_TOKEN=your-secret ./apix-engine

# Works but prints a startup warning
echo 'auth_token: "your-secret"' >> internal/config/config.yaml
./apix-engine
```

For long-running deployments use a systemd `EnvironmentFile`:

```ini
# /etc/apix/secrets
APIX_AUTH_TOKEN=your-secret
```

```ini
# /etc/systemd/system/apix.service
[Service]
EnvironmentFile=/etc/apix/secrets
ExecStart=/usr/local/bin/apix-engine
```

## Remote Engine (vscode.dev)

APiX works in the browser via vscode.dev by connecting to a remotely hosted engine:

1. Run the engine on your server with TLS enabled and an auth token:
   ```bash
   APIX_AUTH_TOKEN=your-secret ./apix-engine
   ```
   Alternatively set `auth_token` in `config.yaml` (a startup warning will remind you to prefer the env var).
2. In VS Code settings, configure:
   - `apix.engine.host` — your server hostname
   - `apix.engine.grpcPort` — gRPC port (default `9090`)
   - `apix.engine.tlsEnabled` — `true`
   - `apix.engine.authToken` — your token
3. Open VS Code at [vscode.dev](https://vscode.dev) and install the extension

## Roadmap

**v0.2**
- [ ] WebSocket traffic inspection
- [ ] Export/import traffic sessions (HAR format)
- [ ] Breakpoint conditions (match on headers/body)
- [ ] UI panel for plugin configuration
- [ ] `make ext-install` without manual `.vsix` step

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
