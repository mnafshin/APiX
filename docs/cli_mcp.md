# CLI Strategy and MCP Integration

## Overview

The APiX CLI is implemented as a thin gRPC client on top of the engine API. The engine now also exposes a dedicated MCP HTTP endpoint (`POST /mcp`) for AI assistants.

## MCP server

APiX can expose MCP tools directly from the engine process:

- `apix.status` (read-only)
- `apix.history.query` (read-only)
- `apix.replay.request` (optional, side effects)
- `apix.compose.request` (optional, side effects)

### Config

```yaml
mcp_enabled: true
mcp_port: "9093"
mcp_bind_address: "127.0.0.1"
mcp_allow_replay: false
mcp_allow_compose: false
```

### Security model

- MCP is **disabled by default**.
- Default bind is loopback (`127.0.0.1`).
- If `auth_token`/`APIX_AUTH_TOKEN` is set, MCP requires `Authorization: Bearer <token>`.
- For remote bind addresses, APiX config validation requires **TLS enabled + auth token**.
- Replay/compose tools are opt-in via `mcp_allow_replay` and `mcp_allow_compose`.

### Example request

```bash
curl -sS http://127.0.0.1:9093/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```
