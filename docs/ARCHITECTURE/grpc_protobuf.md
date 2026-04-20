# gRPC and Protobuf Contract

The APiX engine exposes one gRPC service (`Engine`) as the control plane for:
- live capture streams
- breakpoint workflows
- replay/compose workflows
- history export/import
- rewrite-rule and plugin management

## Source of truth and generated artifacts

- **Proto source:** `pkg/api/proto/apix.proto`
- **Generated Go:** `pkg/api/generated/apix.pb.go`, `pkg/api/generated/apix_grpc.pb.go`
- **Server implementation:** `internal/server/*.go`
- **Extension proto copy:** `apix-vscode/proto/apix.proto`

Never hand-edit files under `pkg/api/generated/`; regenerate instead.

## Service surface (high-level)

`service Engine` contains both unary and server-streaming methods.

Examples:

- Unary:
  - `GetStatus`
  - `ReplayRequest`
  - `ComposeRequest`
  - `ListPlugins`
  - `ClearHistory`
- Streaming:
  - `CaptureTraffic` → `stream HttpRequest`
  - `WatchPausedRequests` → `stream PausedRequest`
  - `GetHistory` → `stream HttpTransaction`
  - `GetWebSocketFrames` → `stream WebSocketFrame`

## Key message types

### `HttpRequest`

Represents captured or synthesized request payload:
- method/url
- headers map
- raw body bytes
- capture timestamp
- request id + protocol

### `HttpResponse`

Status code/text + headers + body payload.

### `HttpTransaction`

Request + optional response + timing metadata + GraphQL metadata + logical `request_id` correlation field.

### `PausedRequest` + `ResumeAction`

Breakpoint control plane:
- engine streams paused requests
- client responds with action (`FORWARD`, `DROP`, `RESPOND`) and optional modifications

### `ReplaySpec`

Supports replay by stored `request_id` or inline `raw_request`, with header/body override controls.

## Streaming patterns and lifecycle

### `CaptureTraffic`

Server side (`internal/server/handlers_traffic.go`) subscribes to engine pub/sub (`engine.Subscribe()`), then forwards each event to stream consumers until cancellation.

Typical clients:
- CLI `watch`
- VS Code traffic panel live stream

### `WatchPausedRequests`

Server streams breakpoint hits from the breakpoint manager. VS Code extension opens `RequestEditor` when events arrive, then calls `ResumeRequest`.

### `GetHistory`

Server executes DB query with filters/pagination and streams historical transactions to avoid huge unary payloads.

## Server wiring and cross-cutting concerns

`internal/server/grpc.go` centralizes server options:

- unary + stream logging interceptors
- optional auth-token interceptors
- optional per-peer rate limiting
- optional TLS credentials
- max metadata header-list size bounds

Reflection is enabled only in unauthenticated local mode.

## Proto evolution rules in APiX

1. Prefer additive fields over breaking changes.
2. Keep field numbers stable.
3. Avoid reusing removed field numbers.
4. Preserve enum semantics once shipped.
5. Update consumers (CLI + extension) for new fields.

## Regeneration workflow

After editing `pkg/api/proto/apix.proto`:

```bash
protoc --go_out=pkg/api/generated --go-grpc_out=pkg/api/generated \
  --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
  pkg/api/proto/apix.proto
cp pkg/api/proto/apix.proto apix-vscode/proto/apix.proto
```

Then run project checks (`make lint`, `make test`, extension compile path when applicable).

## TypeScript proto-loader behavior

The VS Code extension uses dynamic loading (`@grpc/proto-loader`) rather than generated TS stubs.

Important option behavior:
- `keepCase: false` means `snake_case` proto fields become `camelCase` in TS.
  - Example: `request_id` -> `requestId`

This is why extension types and mapping code in `apix-vscode/src/engineClient.ts` must explicitly map camel-cased fields.

## Where to modify when adding an RPC

1. Add message + method in `pkg/api/proto/apix.proto`.
2. Regenerate Go code.
3. Implement handler in `internal/server/handlers_*.go`.
4. Wire client methods:
   - CLI (`cmd/apix-cli/main.go`)
   - VS Code client (`apix-vscode/src/engineClient.ts`)
5. Update docs/contracts as needed (`docs/REFERENCE/*`).
