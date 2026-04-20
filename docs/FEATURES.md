# APiX v2.0.0 — Feature Reference

This document describes all core features and capabilities in APiX v2.0.0. For getting started, see [`getting-started.md`](getting-started.md).

---

## Core Proxy Features

### HTTP/HTTPS Interception

APiX acts as a **Man-in-the-Middle (MITM) proxy** capturing all traffic flowing through it.

- **HTTP proxy** listens on `:8080` (configurable)
- **HTTPS interception** via dynamic TLS certificate generation
- **CA certificate** auto-installation for transparent HTTPS inspection
- **Request/response capture** into persistent SQLite storage

**Usage:**
```bash
curl -x http://localhost:8080 https://httpbin.org/get
```

See [`ARCHITECTURE/proxy_mitm.md`](ARCHITECTURE/proxy_mitm.md) for implementation details.

---

### WebSocket Inspection

Full-duplex **WebSocket message capture** and replay.

- Intercept WebSocket upgrades (HTTP → WS)
- Capture frames in both directions (client→server, server→client)
- Store message history in SQLite
- Replay WebSocket sessions

**Usage in CLI:**
```bash
./apix-cli history list       # includes WebSocket sessions
./apix-cli replay --id <ws-id> # replay WebSocket session
```

---

## Traffic Inspection & Management

### Traffic Capture & History

Every HTTP/HTTPS/WebSocket transaction is **automatically captured** and stored.

- **Full request/response** (headers, body, status code, timing)
- **Persistent storage** via SQLite (survives restarts)
- **History API** for querying and clearing transactions

**CLI commands:**
```bash
./apix-cli history list              # show last 20 transactions
./apix-cli history get <id>          # get details of transaction <id>
./apix-cli history clear             # clear all stored history
```

---

### Breakpoints & Request Interception

**Pause requests matching a pattern** and interactively modify them before forwarding.

- **URL pattern matching** using regex or substring match
- **Pause on match** to inspect/edit requests before they reach the server
- **Three resume actions:**
  - `FORWARD` — send modified request to server
  - `DROP` — silently discard request (return 0 bytes)
  - `RESPOND` — return a custom response without hitting server

**CLI commands:**
```bash
./apix-cli breakpoints add --pattern "api.example.com"
./apix-cli breakpoints list
./apix-cli paused watch              # stream paused requests
./apix-cli paused forward <id>       # forward paused request
./apix-cli paused drop <id>          # drop request
./apix-cli paused respond <id> --body "mock response"
```

**Example:**
```bash
# Pause all requests to example.com
./apix-cli breakpoints add --pattern "example.com"

# Send a request that matches
curl -x http://localhost:8080 https://api.example.com/users

# In another terminal, forward the paused request
./apix-cli paused watch
./apix-cli paused forward <request-id>
```

See [`how-to/first-breakpoint.md`](how-to/first-breakpoint.md) for a walkthrough.

---

### Request Replay

**Re-send captured requests** to test server behavior, compare responses, or stress-test.

- Replay original request as-is
- Modify before replaying (headers, body, method, URL)
- Compare original vs. replayed response
- Stream replay events in real-time

**CLI commands:**
```bash
./apix-cli history list                          # find request ID
./apix-cli replay --id <req-id>                  # replay as-is
./apix-cli replay --id <req-id> --header "X-New:value"  # modify
./apix-cli replay --id <req-id> --body '{"new":"payload"}'
```

See [`how-to/replay.md`](how-to/replay.md) for step-by-step guide.

---

### Request/Response Templates

**Save and reuse request patterns** for frequently-used workflows.

- Save templates from captured requests
- Modify and re-send without manual editing
- Share templates across team

**CLI commands:**
```bash
./apix-cli templates save --id <req-id> --name "get-user"
./apix-cli templates list
./apix-cli templates delete <name>
```

---

## Advanced Features

### Plugin System

APiX contains **14 built-in plugin implementations** in `internal/pluginrt/builtins/`.
At runtime, the engine currently registers and executes:

- `header_editor`
- `mock_response`
- `env_subst`

The remaining built-in plugin implementations are present in source and tests, but are
not yet wired into the runtime request pipeline.

| Plugin | Purpose | Config |
|--------|---------|--------|
| **Rate Limiter** | Request rate limiting (tokens/sec) | `rate_limiter.max_requests_per_second` |
| **Traffic Shaping** | Latency injection / bandwidth throttling | `traffic_shaping.delay_ms`, `bandwidth_kbps` |
| **JWT Auth** | JWT token validation and claims inspection | `jwt_auth.secret`, `jwt_auth.algorithms` |
| **Header Editor** | Add/remove/modify request/response headers | `header_editor.rules` |
| **Latency Modifier** | Add artificial delays to responses | `latency_modifier.delay_ms` |
| **Retry Policy** | Auto-retry failed requests with backoff | `retry_policy.max_retries`, `backoff_multiplier` |
| **Caching** | Response caching with TTL | `caching.ttl_seconds`, `cache_key_headers` |
| **Circuit Breaker** | Fail-fast on repeated errors | `circuit_breaker.failure_threshold`, `timeout_seconds` |
| **Fault Injection** | Simulate failures (errors, latency, drops) | `fault_injection.error_rate`, `error_code` |
| **Policy Engine** | Apply conditional rules (allow/block/modify) | `policy_engine.rules` |
| **OpenTelemetry** | Export traces to OTEL collector | `otel_tracing.exporter_endpoint` |
| **Load Generator** | Synthetic traffic generation | `load_generator.requests_per_second` |
| **Env Subst** | Replace `{{ENV_VAR}}` placeholders in request components | `env_subst.*` |
| **Mock Response** | Return synthetic responses for matching requests | `mock_response.*` |

**Configuration (in `config.yaml`):**
```yaml
plugins:
  rate_limiter:
    enabled: true
    max_requests_per_second: 100
  
  traffic_shaping:
    enabled: true
    delay_ms: 50
    bandwidth_kbps: 1000
```

**CLI inspection:**
```bash
./apix-cli plugins      # list all loaded plugins
```

---

### GraphQL Introspection & Debugging

**Inspect GraphQL schemas** and test queries directly through APiX.

- Capture GraphQL queries in captured traffic
- Introspect schema via captured responses
- Test mutations with modified payloads

**Usage:**
Send GraphQL request through proxy:
```bash
curl -x http://localhost:8080 https://api.example.com/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ user { id name } }"}'
```

Inspect via CLI:
```bash
./apix-cli history list
./apix-cli history get <graphql-request-id>
```

---

### MCP (Model Context Protocol) Integration

**Connect APiX to AI assistants** and code editors via the MCP protocol.

- **MCP server** running on configurable port (default: `:9093`)
- Exposes captured traffic and replay engine to AI clients
- Enables Claude, VS Code extensions, and other MCP-compatible tools to:
  - Query traffic history
  - Trigger replays
  - Analyze patterns
  - Suggest optimizations

**Usage:**
```bash
# MCP server starts with the engine when enabled in config
./apix-engine

# Connect MCP client to localhost:9093
```

See [`REFERENCE/cli_mcp.md`](REFERENCE/cli_mcp.md) for MCP API reference.

---

### Configuration Validation

**Pre-flight checks** ensure configuration is valid before starting.

Validates:
- Port availability (8080, 9090 not in use)
- Plugin paths exist and are readable
- Regex patterns in breakpoints are syntactically valid
- TLS cert paths accessible
- Plugin config schemas match expected types

**Usage:**
```bash
./apix-cli --config-check

# Output: "✅ All checks passed"
# or: "❌ Port 8080 already in use"
```

Exits 0 on success, 1 on failure.

---

### CA Certificate Management

**Automatic TLS certificate generation** for transparent HTTPS interception.

- Creates a **local CA certificate** on first run
- Stores in `~/.apix/ca.crt` and `~/.apix/ca.key`
- Generates per-domain certificates on-the-fly
- On macOS: auto-installs to system keychain (with prompt)
- On Linux/Windows: manual installation via `./apix-cli cert install`

**Commands:**
```bash
./apix-cli cert show                 # display CA certificate
./apix-cli cert install              # install CA to system trust store
./apix-cli cert export --out cert.pem  # export CA cert
```

See [`ARCHITECTURE/proxy_mitm.md`](ARCHITECTURE/proxy_mitm.md) for CA setup details.

---

## Integration Points

### VS Code Extension

Graphical interface for all APiX features:
- Traffic tree view with search
- Breakpoint editor
- Request/response viewer
- Replay builder
- Real-time transaction stream

Install from VS Code marketplace or build from source:
```bash
cd apix-vscode && npm run compile
```

Then `Run Extension` in VS Code.

---

### gRPC API

**Programmatic access** to all APiX functions via gRPC.

Exported services:
- `Engine.CaptureTraffic()` — stream captured transactions
- `Engine.GetHistory()` — query stored requests/responses
- `Engine.StoreTransaction()` — manual transaction storage
- `Breakpoints.List/Add/Delete/Enable()` — manage breakpoints
- `Paused.Watch/Forward/Drop/Respond()` — handle paused requests
- `Replay.Send()` — replay stored requests
- `Templates.Save/List/Delete()` — manage templates

**Client setup (Go):**
```go
import "github.com/mnafshin/apix/pkg/api/generated"

conn, _ := grpc.Dial("localhost:9090", grpc.WithInsecure())
client := generated.NewEngineClient(conn)
```

See [`REFERENCE/cli-contract-v1.md`](REFERENCE/cli-contract-v1.md) for full API spec.

---

### CLI (apix-cli)

**Command-line interface** for all APiX operations.

Commands:
```
status                   # engine health and version
plugins                  # list loaded plugins
history list|get|clear   # transaction history
breakpoints list|add|delete|enable|disable
paused watch|forward|drop|respond
templates save|list|delete
replay                   # replay stored requests
config                   # show/validate config
doctor                   # diagnostic info
completion               # shell completion
cert show|install|export # CA certificate management
send                     # send custom HTTP request
```

See `./apix-cli help` for detailed options.

---

## Configuration Reference

Key configuration options in `config.yaml`:

```yaml
# Proxy ports
http_port: 8080
grpc_port: 9090
grpc_bind_address: 127.0.0.1

# Storage
db_path: ~/.apix/apix.db

# TLS/HTTPS
tls_enabled: false
grpc_cert_path: /etc/apix/grpc-server.pem
grpc_key_path: /etc/apix/grpc-server-key.pem
ca_cert_path: ~/.apix/ca.pem
ca_key_path: ~/.apix/ca-key.pem

# Runtime limits and controls
max_body_size_mb: 32
breakpoint_pause_timeout_sec: 120
grpc_rate_limit_per_sec: 0

# MCP
mcp_enabled: false
mcp_port: 9093
mcp_bind_address: 127.0.0.1
mcp_allow_replay: false
mcp_allow_compose: false
```

See [`CONFIG_VALIDATION.md`](CONFIG_VALIDATION.md) for complete schema.

---

## Performance & Limitations

| Feature | Capacity | Notes |
|---------|----------|-------|
| **Concurrent connections** | Target: 1000+ | Depends on host resources; benchmark data pending (#300) |
| **History size** | Target: 100k+ transactions | Depends on workload/storage; benchmark data pending (#300) |
| **Plugin overhead** | Target: <5ms per request | Depends on plugin chain; benchmark data pending (#300) |
| **Breakpoint patterns** | Not hard-capped in config | Practical limits depend on regex complexity and memory |
| **Paused requests** | 100 (configurable) | FIFO when limit exceeded |

---

## Troubleshooting

**Port already in use:**
```bash
./apix-cli --config-check  # diagnose
lsof -i :8080              # see what's using port
```

**HTTPS not intercepting:**
```bash
./apix-cli cert show       # verify CA cert exists
./apix-cli cert install    # install to system trust store
```

**Plugins not loading:**
```bash
./apix-cli doctor          # full diagnostic
./apix-engine --config-check
```

---

## See Also

- [`getting-started.md`](getting-started.md) — workflow-first guides
- [`ARCHITECTURE/design-principles.md`](ARCHITECTURE/design-principles.md) — design overview
- [`CONFIG_VALIDATION.md`](CONFIG_VALIDATION.md) — configuration reference
- [`REFERENCE/cli-contract-v1.md`](REFERENCE/cli-contract-v1.md) — CLI/gRPC API
- [`DEPLOYMENT.md`](DEPLOYMENT.md) — production deployment
