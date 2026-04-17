![APiX](public/assets/img/APiX.png)

# APiX — API Debugger

> Intercept, inspect, and debug HTTP/HTTPS traffic directly in VS Code.

![Release](https://img.shields.io/badge/release-v1.0.0-green.svg)
![Build](https://github.com/mnafshin/apix/actions/workflows/ci.yml/badge.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.24+-00ADD8.svg)
![TypeScript](https://img.shields.io/badge/typescript-5.0+-3178C6.svg)
![VS Code](https://img.shields.io/badge/vscode-%5E1.85-007ACC.svg)
![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey.svg)

## What is APiX?

APiX is an API debugging toolkit backed by a Go proxy engine. It intercepts HTTP/HTTPS traffic via a MITM proxy, lets you pause requests at URL breakpoints, edit and replay them, and extend behavior with plugins. It now ships both a VS Code extension and a contract-first CLI on the same gRPC engine API.

## Features

- 🔌 **HTTP/HTTPS intercepting proxy** with MITM support
- 🛑 **URL breakpoints** — pause, inspect, edit, and resume requests
- 🔁 **Request replay** with header/body overrides
- 📤 **Traffic portability** — HAR export/import and copy-as-curl from the VS Code workflow
- 🔄 **WebSocket inspection** — capture upgrade handshakes and inspect persisted frames
- 💻 **Contract-first CLI** — status, history, watch, breakpoints, paused actions, replay, doctor, setup, and completion
- 🧩 **Plugin system** — HeaderEditor, MockResponse, EnvSubst (and custom)
- 💾 **SQLite persistent storage** — traffic history survives restarts
- 🖥️ **VS Code extension** — traffic inspector and breakpoints view in the sidebar
- 🌐 **Browser support** (vscode.dev) via remote engine over TLS — _planned, see #11_
- 📦 **Cross-platform** — macOS, Linux, Windows

## Quick Start

### Download & Run (macOS, Linux, Windows)

**Option 1: Direct Binary Download**

```bash
# macOS (ARM64)
curl -L https://github.com/mnafshin/APiX/releases/download/v1.0.0/apix-engine-darwin-arm64 -o apix-engine
chmod +x apix-engine && ./apix-engine

# macOS (Intel)
curl -L https://github.com/mnafshin/APiX/releases/download/v1.0.0/apix-engine-darwin-amd64 -o apix-engine
chmod +x apix-engine && ./apix-engine

# Linux (x86_64)
curl -L https://github.com/mnafshin/APiX/releases/download/v1.0.0/apix-engine-linux-amd64 -o apix-engine
chmod +x apix-engine && ./apix-engine

# Linux (ARM64)
curl -L https://github.com/mnafshin/APiX/releases/download/v1.0.0/apix-engine-linux-arm64 -o apix-engine
chmod +x apix-engine && ./apix-engine

# Windows
# Download: https://github.com/mnafshin/APiX/releases/download/v1.0.0/apix-engine-windows-amd64.exe
```

**Option 2: Go Install (CLI users)**

If you have Go 1.21+ installed, you can install the engine and CLI directly:

```bash
go install github.com/mnafshin/apix/cmd/apix-engine@latest
go install github.com/mnafshin/apix/cmd/apix-cli@latest
```

**Option 3: VS Code Extension**

Search for **APiX** by `mnafshin` in the VS Code Marketplace, or install the `.vsix` file manually:

```bash
cd apix-vscode
npm install
npm run compile
npm run package
code --install-extension apix-1.0.0.vsix
```

**Option 3: Docker** _(image not yet published — build from source below)_

```bash
# Build the image locally:
docker build -t apix:local -f build/Dockerfile .
docker run -p 8080:8080 -p 9090:9090 apix:local
```

**Option 4: Build from Source**

```bash
# Clone repository
git clone https://github.com/mnafshin/APiX.git && cd APiX

# Build engine + CLI
make build
make build-cli

# Run engine (HTTP proxy :8080, gRPC :9090)
./apix-engine

# Check engine from the CLI
./apix-cli status
```

### Configure Your Client

Point your HTTP client at `http://localhost:8080`:

```bash
# macOS/Linux
export http_proxy=http://localhost:8080
export https_proxy=http://localhost:8080
curl https://api.example.com/endpoint

# Browser
# Settings → Network → HTTP Proxy: localhost:8080

# Docker client
# Configure proxy in ~/.docker/config.json
```

Then open VS Code and the APiX extension will automatically start the engine and show captured traffic in the sidebar.

### Traffic Portability

APiX can now move captured traffic in and out of the tool without custom scripts:

- **Copy as curl** from a traffic item or the traffic inspector panel
- **Export Traffic as HAR** for a single request or the full stored history
- **Import HAR File** to load requests into history so they can be inspected and replayed

### WebSocket Inspection

APiX now captures upgraded `ws://` and `wss://` sessions as first-class traffic entries:

- **Inspect the `101 Switching Protocols` handshake** alongside regular HTTP history
- **View persisted client/server frame streams** in the VS Code traffic inspector
- **Support both cleartext `ws://` and MITM-intercepted `wss://` traffic**

**See [DEPLOYMENT.md](docs/DEPLOYMENT.md) for production deployment (Kubernetes, systemd, Docker Compose).**

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

### Building Locally

```bash
# Build engine + CLI
make build
make build-cli

# Build the VS Code extension
make ext-build

# Run repository Go tests
make test

# Run focused race tests when needed
go test -race ./internal/proxy

# Run a single test package
go test ./tests/integration/... -v

# Run benchmarks
go test -bench=. -benchmem ./internal/proxy ./internal/storage ./internal/breakpoints

# Run tests with coverage report
make test-coverage

# Lint
make lint

# Regenerate protobuf code
make proto

# Cross-compile engine for all platforms
make build-all
```

### Testing

APiX has multi-layer coverage across unit, integration, E2E, contract,
resilience, stateful workflow, MCP, and benchmark suites:

```bash
# All tests
go test ./... -race

# Integration tests only
go test ./tests/integration/...

# Run benchmarks
go test -bench=. -benchmem ./internal/proxy ./internal/storage ./internal/breakpoints
```

**Test coverage by layer:**

| Layer | Coverage | Key Tests |
|-------|----------|-----------|
| **Unit tests** | Core packages | Config, plugins, breakpoints, storage, engine |
| **Integration tests** | Proxy + gRPC flows | Proxy→storage pipeline, gRPC auth, replay engine |
| **E2E tests** | Full-stack scenarios | Breakpoint actions, plugin mutations, concurrent traffic |
| **Contract tests** | API and generated-code integrity | Proto sync, generated-code freshness |
| **Stateful / resilience / MCP tests** | Workflow and failure coverage | Streams, reconnects, engine restarts, transcript behavior |
| **Benchmarks** | Performance baselines | HTTP proxy, storage, breakpoints |

**Results:**
- ✅ Go, extension, and release-smoke workflows run in CI
- ✅ Race detector: clean
- ✅ TypeScript type checks: passing
- ✅ Performance baselines established for CI regression detection

**Next testing layers for CLI and MCP:**
- stateful workflow tests for breakpoint/pause/replay/rule lifecycles
- contract and transcript regression tests for machine-readable CLI and MCP behavior
- resilience and fault-injection tests for streams, reconnects, and engine restarts
- release smoke tests across engine, extension, and future CLI artifacts

**See [TESTING.md](docs/TESTING.md) for comprehensive test strategy and how to contribute tests.**

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
| `GetWebSocketFrames` | Server-stream | Retrieve persisted WebSocket frames for a captured upgrade request |
| `ClearHistory` | Unary | Delete all stored traffic history |
| `ExportHAR` | Unary | Export one or more stored transactions as HAR 1.2 JSON |
| `ImportHAR` | Unary | Import HAR 1.2 traffic into stored history |

Proto definition: [`pkg/api/proto/apix.proto`](pkg/api/proto/apix.proto)

`SetBreakpoint` validates its input: `url_pattern` is required and must not exceed 500 characters; `methods` must be standard HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, `CONNECT`, `TRACE`). Invalid input returns `codes.InvalidArgument`.

## Plugins

Built-in plugins live in `internal/pluginrt/builtins/`:

| Plugin | Description |
|--------|-------------|
| `HeaderEditor` | Add, modify, or remove request/response headers |
| `MockResponse` | Return a synthetic response without hitting upstream |
| `EnvSubst` | Replace `${VAR}` placeholders in request bodies with environment values |

**Custom plugins** implement the `Plugin` interface in `pkg/plugins/sdk.go`:

```go
type Plugin interface {
    Name() string
    Version() string
    Description() string
    OnRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error)
    OnResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error)
}
```

For a custom plugin, implement the interface in your own package and register
it with the runtime in `cmd/apix-engine/main.go`. See
[CONTRIBUTING.md](CONTRIBUTING.md) for a step-by-step guide.

## Configuration

All settings live in `internal/config/config.yaml`. The file is optional — defaults are used when it is absent.

Configuration validation: run `apix-engine --config-check` to validate your configuration and exit (see docs/CONFIG_VALIDATION.md).

| Key | Default | Description |
|-----|---------|-------------|
| `http_port` | `8080` | HTTP proxy listen port |
| `grpc_port` | `9090` | gRPC API listen port |
| `grpc_bind_address` | `127.0.0.1` | gRPC server bind address (defaults to loopback; widen only for secured remote access) |
| `db_path` | `apix.db` | SQLite database path |
| `ca_cert_path` | `~/.apix/ca.pem` | MITM CA certificate |
| `ca_key_path` | `~/.apix/ca-key.pem` | MITM CA private key |
| `tls_enabled` | `false` | Enable TLS on the gRPC server |
| `auth_token` | `""` | Bearer token for gRPC auth (prefer `APIX_AUTH_TOKEN` env var) |
| `max_idle_conns_per_host` | `10` | Max idle upstream connections per host |
| `idle_conn_timeout_sec` | `90` | Seconds an idle upstream connection stays open |
| `dial_timeout_sec` | `10` | Seconds allowed for a TCP dial to upstream |
| `upstream_tls_handshake_timeout_sec` | `10` | TLS handshake timeout for upstream connections |
| `upstream_response_header_timeout_sec` | `30` | Timeout waiting for upstream response headers |
| `upstream_expect_continue_timeout_sec` | `1` | Timeout for `100-continue` handling upstream |
| `http_read_header_timeout_sec` | `10` | HTTP header read timeout (DoS protection) |
| `http_read_timeout_sec` | `30` | HTTP read timeout (DoS protection) |
| `http_write_timeout_sec` | `120` | HTTP write timeout (large response bodies) |
| `http_idle_timeout_sec` | `120` | Idle timeout for client-side HTTP connections |
| `max_body_size_mb` | `32` | Maximum buffered request body size in MB (`0` disables the limit) |
| `replay_skip_tls_verify` | `false` | Skip TLS verification for replayed requests (testing only) |
| `metrics_enabled` | `false` | Enable Prometheus metrics endpoint |
| `metrics_port` | `9091` | Port for the Prometheus metrics endpoint |
| `slowlog_threshold_ms` | `1000` | Threshold for slow-request logging |
| `plugin_paths` | `[]` | Extra plugin artifact paths validated at startup |
| `url_patterns` | `[]` | Preconfigured URL regex patterns validated at startup |

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

### v1.0.0 ✅ (Current)
- ✅ HTTP/HTTPS interception and MITM
- ✅ WebSocket inspection for `ws://` and `wss://`
- ✅ gRPC API for remote control
- ✅ URL breakpoints (pause, inspect, edit, resume)
- ✅ Request replay engine
- ✅ Plugin system (3 built-ins: HeaderEditor, MockResponse, EnvSubst)
- ✅ SQLite persistent storage
- ✅ VS Code extension
- ✅ Multi-platform binaries (macOS, Linux, Windows)
- ✅ Comprehensive deployment guide
- ✅ Security hardening (TLS, auth tokens, secure defaults)

### v1.0.1 (Patch - ~2 weeks)
- [ ] Bug fixes from user feedback
- [ ] Performance optimizations
- [ ] Additional logging/debugging options

### v1.1 (Minor - ~4-6 weeks)
- [x] Contract-first CLI foundation (`cmd/apix-cli`, shared transport/auth config, stable flags)
- [x] AI-ready output contracts (`--output json`, NDJSON streams, stable exit codes)
- [x] Initial CLI read flows (`status`, `plugins`, `history`, `watch`)

### v1.2 (Minor - ~6-8 weeks)
- [x] Core CLI terminal workflows (`history`, `watch`, breakpoints, paused request actions, replay/send)
- [x] CLI operator tooling (`doctor`, certificate status/help, shell completion)
- [x] History management and export/import foundations
- [x] Copy-as-curl in the VS Code traffic workflow
- [x] WebSocket traffic inspection
- [ ] Breakpoint conditions (match on headers, body, status code)
- [x] HAR export/import (session persistence)
- [ ] Response mocking UI

### v1.3 (Minor)
- [ ] Declarative rewrite rules and repeatable mocking workflows
- [ ] Request composition/templating
- [ ] MCP integration and AI-facing automation surfaces
- [ ] Map Local and cURL/HAR portability improvements

### v1.4+ (Protocol Expansion)
- [ ] gRPC-over-HTTP/2 inspection
- [ ] HTTP/2 and staged HTTP/3 support
- [ ] API authentication/authorization testing
- [ ] Performance profiling
- [ ] Distributed tracing support (OpenTelemetry)
- [ ] Homebrew formula
- [ ] Snap package
- [ ] AUR package (Arch Linux)

### v2.0 (Major - ~3-4 months)
- [ ] Full-featured test suite UI
- [ ] CI/CD integration (GitHub Actions, GitLab CI, Jenkins)
- [ ] Scriptable request/response transformations
- [ ] Multi-engine clustering
- [ ] Custom plugin marketplace
- [ ] Analytics dashboard

## CLI Direction

APiX keeps **gRPC as the engine control plane** for both the VS Code extension and the CLI. That remains the right backend boundary: one API surface for streaming, auth, remote access, and feature parity.

The CLI will add a separate **user contract**, not a separate backend protocol. The first milestone is a terminal workflow that is:

- **Human-friendly**: concise defaults, readable tables/text, good help, and explicit troubleshooting
- **Automation-friendly**: stable flags, deterministic exit codes, and script-safe non-interactive behavior
- **AI-ready**: machine-readable JSON and NDJSON output modes with stable field names and no need to scrape prose

The current compatibility surface is documented in [`docs/cli-contract-v1.md`](docs/cli-contract-v1.md).

If browser-native or third-party HTTP integrations become a primary requirement later, APiX can add a thin gateway on top of gRPC. That should be a later compatibility layer, not a replacement for the engine API.

### Suggested Rollout Plan

APiX's recommended rollout sequence is:

1. **CLI foundation** — shipped with shared transport/auth config, JSON output, NDJSON streams, and stable exit codes
2. **Core terminal workflows** — shipped with `history`, `watch`, breakpoint management, paused request actions, `replay`, `send`, and operator tooling
3. **Agent-ready workflows** — next: add declarative rules, request composition, import/export portability, and MCP integration
4. **Protocol expansion** — add broader HTTP/2 visibility and staged HTTP/3 support

This ordering is intentional: first make the CLI contract solid, then make it useful without the extension, then make it automatable for agents, and only then broaden protocol coverage.

## Supported Versions

### Release Support Policy

| Version | Status | Release Date | Support Until | Notes |
|---------|--------|--------------|---------------|-------|
| v1.0.0 | **Current** | Apr 8, 2026 | Apr 8, 2027 (12 months LTS) | Initial release; suited for development use — not yet battle-tested at scale |

### System Requirements

- **Go**: 1.24+
- **VS Code**: 1.85+
- **Python**: (Optional) 3.8+ for plugin development
- **Platforms**: macOS (10.14+), Linux (glibc 2.29+), Windows (10+)

## Performance Characteristics

> ⚠️ The figures below are informal estimates. No automated benchmarks have been run yet; actual performance depends on payload size, host hardware, and concurrency. See [#169](https://github.com/mnafshin/APiX/issues/169) for the tracking issue.

- **Max concurrent connections**: 1000 (configurable via OS and Go runtime)
- **Max request size per body**: controlled by `max_body_size_mb` (default 32 MB)
- **Request history**: SQLite-backed (no hard limit by default)
- **Memory footprint**: ~50 MB baseline + buffered body size (see Memory Model below)
- **Latency overhead**: typically a few milliseconds on local loopback (not benchmarked)
- **Throughput**: not formally benchmarked; proxy adds minimal overhead for typical dev workloads

### Memory Model

APiX buffers **entire request and response bodies** into memory via `io.ReadAll`. This is required for body inspection, plugin mutation, replay, and storage — but it has aggregate memory implications you should understand before deploying at scale:

| Scenario | Memory per request | 100 concurrent requests |
|---|---|---|
| Default (`max_body_size_mb: 32`) | up to 64 MB (req + resp) | up to 6.4 GB peak |
| Small API traffic (`max_body_size_mb: 1`) | up to 2 MB | up to 200 MB peak |
| Recommended dev default | `max_body_size_mb: 10` | up to 2 GB peak |

**Key points:**
- Both the **request body** (`internal/proxy/http.go`, `https.go`) and the **response body** are buffered simultaneously for each in-flight request.
- The `max_body_size_mb` limit is **per individual body** (not per transaction). Each transaction may buffer up to `2 × max_body_size_mb` while in flight (request + response).
- Bodies are stored in SQLite after capture, consuming additional disk. There is no automatic retention expiry by default — use `ClearHistory` or set a periodic vacuum interval.
- Setting `max_body_size_mb: 0` disables the size limit entirely (dangerous in high-traffic environments).

**Tuning recommendations:**
```yaml
# config.yaml — for high-concurrency or memory-constrained environments
max_body_size_mb: 5       # 5 MB per body; 10 MB per transaction peak
vacuum_interval_hours: 6  # reclaim SQLite space every 6 hours
```

## Comparison with Alternatives

| Feature | APiX | mitmproxy | Charles | Fiddler | VS Code Built-in |
|---------|------|-----------|---------|---------|------------------|
| **Platform** | macOS, Linux, Windows | All | macOS, Windows | Windows | VS Code only |
| **IDE Integration** | ✅ VS Code | ❌ Separate | ❌ Separate | ❌ Separate | ✅ Built-in |
| **HTTP/HTTPS** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Limited |
| **WebSocket** | ✅ Yes | ✅ Yes | ❌ No | ✅ Yes | ❌ No |
| **Request Replay** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ❌ No |
| **Breakpoints** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes | ❌ No |
| **Scripting** | 🔄 v1.1+ | ✅ Python | ❌ No | ✅ .NET | ❌ No |
| **Open Source** | ✅ Apache 2.0 | ✅ MIT | ❌ Proprietary | ❌ Proprietary | ✅ MIT |
| **Price** | Free | Free | $99 | $99 | Free |

## Telemetry & Privacy

APiX **does not collect telemetry** or send data externally. All traffic stays on your machine. Configuration files and databases are stored locally only.

## Troubleshooting

### Engine won't start
- Check if port 8080 or 9090 is already in use: `lsof -i :8080`
- Ensure you have permissions to create files in `~/.apix/`
- Check engine logs: tail the console output where you ran `./apix-engine`

### Extension doesn't connect
- Ensure the engine is running (`apix-engine` process exists)
- Check gRPC port is 9090 (or adjust in settings)
- Verify engine has correct `config.yaml` path (see Configuration section)
- Reload VS Code window (Cmd+R or Ctrl+Shift+F5)

### Proxy traffic not appearing
- Verify your HTTP client is configured to use `http://localhost:8080` as proxy
- For HTTPS traffic, you may need to install APiX's CA certificate (see DEPLOYMENT.md)
- Check engine logs for proxy errors

### High memory usage
- Export traffic to HAR before clearing history when you need to archive a session
- Clear traffic history: APiX UI → right-click → Clear History
- CLI support for history management is available via `apix history list|get|clear`
- **Lower `max_body_size_mb`** in `config.yaml` — this is the most impactful setting. Default is 32 MB per body; at 100 concurrent requests, peak memory is `2 × max_body_size_mb × concurrent_requests`. Set to `5` or `10` for typical use.
- Enable periodic VACUUM: set `vacuum_interval_hours: 6` in `config.yaml` to reclaim SQLite disk space
- See [Memory Model](#memory-model) for a detailed breakdown of how bodies are buffered

### Certificate validation errors
- For testing: set `replay_skip_tls_verify: true` in `config.yaml`
- For production: install the APiX CA certificate in your system/browser store (see DEPLOYMENT.md)

## Issues & Feedback

Found a bug? Have a feature request? Open a [GitHub Issue](https://github.com/mnafshin/APiX/issues).

For security vulnerabilities, please open a private security advisory on GitHub.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).

## Metrics & OpenTelemetry Collector

APiX exposes Prometheus-format metrics at `/metrics` when
`metrics_enabled: true` is set in `config.yaml`. By default the metrics server
listens on `metrics_port` (`9091`). To integrate with OpenTelemetry, run the
OpenTelemetry Collector configured to scrape APiX and export metrics via OTLP.
See `docs/OTEL.md` and the sample config `docs/otel-collector-config.yaml` for
a quick-start example.

## Project status and claims

The README aims to represent shipped functionality versus roadmap items clearly. Shipped features are those implemented in the default branch; roadmap items are noted in docs/ and the issue tracker. For an up-to-date list of implemented features, see the "Features" section below and check the issue tracker for open RFCs and roadmap items.
