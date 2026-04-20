![APiX](public/assets/img/APiX.png)

# APiX

Intercept, inspect, and modify HTTP/HTTPS traffic with a local MITM proxy, a gRPC control plane, a VS Code extension, and a CLI.

![Build](https://github.com/mnafshin/apix/actions/workflows/ci.yml/badge.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)

## What APiX provides

- **HTTP/HTTPS proxy engine** (`:8080` by default) with MITM interception
- **gRPC API** (`:9090` by default) for extension/CLI automation
- **Breakpoints** with conditions (URL, method, headers, body regex, response status)
- **Replay + compose** requests (`ReplayRequest`, `ComposeRequest`)
- **Saved request templates** (save/list/delete)
- **History storage** in SQLite, plus HAR export/import
- **WebSocket frame capture** for upgraded sessions
- **GraphQL metadata extraction** for request/response inspection
- **Map Local rules** for serving local files on URL matches
- **MCP endpoint** (`/mcp`) with gated side-effect tools
- **Prometheus metrics** (`/metrics`) and health endpoint (`/healthz`)

## Repository layout

- `cmd/apix-engine` — engine binary entrypoint
- `cmd/apix-cli` — CLI client
- `internal/proxy` — HTTP/HTTPS interception layer
- `internal/server` — gRPC and MCP servers
- `internal/engine` — orchestration and request lifecycle
- `internal/storage` — SQLite persistence
- `internal/replay` — replay/compose execution
- `internal/pluginrt` — plugin runtime
- `apix-vscode/` — VS Code extension
- `pkg/api/proto/apix.proto` — API source of truth
- `pkg/api/generated/` — generated Go gRPC types

## Quick start (source build)

```bash
git clone https://github.com/mnafshin/APiX.git
cd APiX

make build
make build-cli

./apix-engine
```

In another terminal:

```bash
./apix status
curl -x http://localhost:8080 https://example.com
```

## VS Code extension (local dev)

```bash
make ext-build
```

Then run the extension from VS Code (`Run Extension`), or package/install from `apix-vscode`.

## CLI surface

`apix` currently includes:

- `status`, `plugins`
- `history list|get|clear`
- `watch`
- `breakpoints list|add|delete|enable|disable`
- `paused watch|forward|drop|respond`
- `send`
- `templates save|list|delete`
- `replay`
- `cert`, `config`, `doctor`, `completion`

Use `apix help` for details.

> Compatibility note: `apix-cli` remains available as a compatibility alias.

## Configuration

Default config file: `internal/config/config.yaml`  
Runtime lookup order:

1. `$APIX_CONFIG`
2. `~/.apix/config.yaml`
3. `/etc/apix/config.yaml`
4. `./config.yaml`

Validate config:

```bash
./apix-engine --config-check
```

### Important keys

| Key | Default | Purpose |
|---|---|---|
| `http_port` | `8080` | Proxy listen port |
| `grpc_port` | `9090` | gRPC API port |
| `grpc_bind_address` | `127.0.0.1` | gRPC bind address |
| `tls_enabled` | `false` | Enable TLS for gRPC |
| `grpc_cert_path` / `grpc_key_path` | empty | Required when TLS is enabled |
| `auth_token` / `auth_token_file` / `auth_token_require_strict_perms` | empty / empty / `true` | Bearer token auth, secret-file support, and strict config permission enforcement |
| `proxy_rate_limit_per_sec` / `proxy_max_concurrent_connections` | `1000` / `200` | Per-client proxy throttling and concurrent tunnel/request cap |
| `max_body_size_mb` | `32` | Request/response body limit per body |
| `history_max_age_days` / `history_max_rows` | `0` / `0` | Retention pruning controls |
| `metrics_enabled` / `metrics_port` | `false` / `9091` | Prometheus metrics endpoint |
| `access_log_enabled` / `access_log_format` / `access_log_path` | `false` / `json` / `stdout` | Per-request access logging |
| `audit_log_enabled` / `audit_log_path` | `false` / `stdout` | Structured audit trail for state-changing gRPC operations |
| `otel_enabled` / `otel_endpoint` / `otel_service_name` / `otel_insecure` / `otel_sample_rate` | `false` / `localhost:4317` / `apix-proxy` / `true` / `1.0` | Built-in OTLP tracing exporter plugin |
| `health_port` | `9092` | `/healthz` endpoint |
| `log_format` / `log_level` | `text` / `info` | Engine log output shape and verbosity |
| `mcp_enabled` / `mcp_port` | `false` / `9093` | MCP server |
| `mcp_allow_replay` / `mcp_allow_compose` | `false` / `false` | Side-effect MCP tools |
| `map_local_rules` | empty | URL regex -> local file response mapping (`file_path`, with `local_path` alias) |

## Security model

- Local defaults bind gRPC/MCP to loopback.
- Remote gRPC and remote MCP are validated to require **TLS + auth token**.
- Prefer `APIX_AUTH_TOKEN` environment variable over storing `auth_token` in plaintext.
- If `auth_token` is stored in `config.yaml`, APiX enforces strict file permissions (0600 or stricter) by default.

## Development commands

```bash
make build          # build engine
make build-cli      # build CLI
make test           # run go test ./...
make lint           # run go vet ./...
make proto          # regenerate gRPC Go code from proto
make ext-build      # build extension + copy engine binary
make build-all      # cross-compile binaries
```

## API contract

The gRPC API contract is defined in:

- `pkg/api/proto/apix.proto` (source of truth)

Generated Go code:

- `pkg/api/generated/apix.pb.go`
- `pkg/api/generated/apix_grpc.pb.go`

Regenerate:

```bash
make proto
```

## Additional docs
See [`docs/INDEX.md`](docs/INDEX.md) for the complete documentation index.

**Quick links:**
- **Getting started:** [`docs/getting-started.md`](docs/getting-started.md)
- **Deployment:** [`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md)
- **Configuration:** [`docs/CONFIG_VALIDATION.md`](docs/CONFIG_VALIDATION.md)
- **Architecture:** [`docs/ARCHITECTURE/`](docs/ARCHITECTURE/)
- **CLI & MCP:** [`docs/REFERENCE/cli_mcp.md`](docs/REFERENCE/cli_mcp.md)
- **Versioning policy:** [`docs/REFERENCE/versioning-policy.md`](docs/REFERENCE/versioning-policy.md)
- **Release gate:** [`docs/RELEASE_GATE.md`](docs/RELEASE_GATE.md)
- **Testing:** [`docs/TESTING.md`](docs/TESTING.md)
- **Contributing:** [`CONTRIBUTING.md`](CONTRIBUTING.md)
- **Security policy:** [`SECURITY.md`](SECURITY.md)

## License

Apache 2.0 — see [LICENSE](LICENSE).
