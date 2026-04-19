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
./apix-cli status
curl -x http://localhost:8080 https://example.com
```

## VS Code extension (local dev)

```bash
make ext-build
```

Then run the extension from VS Code (`Run Extension`), or package/install from `apix-vscode`.

## CLI surface

`apix-cli` currently includes:

- `status`, `plugins`
- `history list|get|clear`
- `watch`
- `breakpoints list|add|delete|enable|disable`
- `paused watch|forward|drop|respond`
- `send`
- `templates save|list|delete`
- `replay`
- `cert`, `config`, `doctor`, `completion`

Use `apix-cli help` for details.

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
| `auth_token` | empty | Bearer token for gRPC/MCP auth |
| `max_body_size_mb` | `32` | Request/response body limit per body |
| `history_max_age_days` / `history_max_rows` | `0` / `0` | Retention pruning controls |
| `metrics_enabled` / `metrics_port` | `false` / `9091` | Prometheus metrics endpoint |
| `health_port` | `9092` | `/healthz` endpoint |
| `mcp_enabled` / `mcp_port` | `false` / `9093` | MCP server |
| `mcp_allow_replay` / `mcp_allow_compose` | `false` / `false` | Side-effect MCP tools |
| `map_local_rules` | empty | URL regex -> local file response mapping |

## Security model

- Local defaults bind gRPC/MCP to loopback.
- Remote gRPC and remote MCP are validated to require **TLS + auth token**.
- Prefer `APIX_AUTH_TOKEN` environment variable over storing `auth_token` in plaintext.

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

- `docs/DEPLOYMENT.md` — deployment patterns
- `docs/CONFIG_VALIDATION.md` — validation rules
- `docs/cli_mcp.md` — MCP usage
- `docs/TESTING.md` — testing strategy
- `CONTRIBUTING.md` — contribution guide

## License

Apache 2.0 — see [LICENSE](LICENSE).
