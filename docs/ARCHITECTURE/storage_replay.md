# Storage, Replay, and Breakpoint Orchestration

APiX persists captured traffic to SQLite, serves it over gRPC history APIs, and can replay stored or synthetic requests through the same HTTP stack.

## Core packages

- `internal/storage/` — SQLite schema and query layer
- `internal/engine/` — write orchestration + pub/sub fanout to live subscribers
- `internal/replay/` — replay request builders and upstream execution
- `internal/breakpoints/` — pause/resume decisions injected into proxy flow

## Data model (SQLite)

Schema source: `internal/storage/schema.go`.

Primary tables:

- `requests` — canonical request record (`id`, method/url/headers/body, timestamp, duration, protocol)
- `responses` — response by `request_id` (1:1 with requests when available)
- `breakpoints` — persistent breakpoint rules
- `plugins` — plugin config and enabled state
- `ws_frames` — WebSocket frame history keyed by `transaction_id`
- `rewrite_rules` — response/request rewrite and mock rules
- `request_templates` — saved replay/compose templates

Indexes include timestamp/method/url and websocket transaction lookups to keep list/history views responsive under load.

## DB initialization and runtime behavior

`storage.Open()` (`internal/storage/sqlite.go`) configures:

- WAL mode (`PRAGMA journal_mode=WAL`)
- `synchronous=NORMAL`
- `busy_timeout=5000`
- `foreign_keys=ON`
- schema bootstrap + lightweight migrations

In-memory mode (`:memory:`) pins connection count to 1 so tests share one database.

## Write path (capture -> persistence)

```text
HTTP/TLS proxy handler
   -> builds proxy.Transaction
   -> engine.StoreTransaction(tx)
       -> map headers/body into storage.RequestRecord + ResponseRecord
       -> SaveTransaction(req, resp) when available (single SQL tx)
          or SaveRequest/SaveResponse fallback
       -> publish HttpRequest to capture subscribers
```

Key files:
- `internal/proxy/http.go`
- `internal/proxy/https.go`
- `internal/engine/engine.go` (`StoreTransaction`)
- `internal/storage/queries.go` (`SaveTransaction`, `SaveRequest`, `SaveResponse`)

## Read path (history APIs)

```text
CLI/VS Code
   -> gRPC GetHistory
   -> internal/server/handlers_traffic.go
   -> storage.ListTransactions(...)
   -> stream HttpTransaction records to client
```

`ListTransactions` supports:
- pagination (`limit`, `offset`)
- URL and method filters
- status-code filter
- request/response body substring filter

WebSocket inspection is similar via `GetWebSocketFrames` + `ListWebSocketFrames`.

## Replay lifecycle

Replay entrypoint: `internal/replay/engine.go` (`ReplayRequest`).

`ReplaySpec` supports two sources:
1. `request_id` (load stored request from DB)
2. `raw_request` (ad-hoc composed request)

Flow:

1. Select source builder (`storedRequestBuilder` or `rawRequestBuilder`).
2. Build `http.Request`.
3. Apply override headers/method/body.
4. Execute with replay HTTP client (timeouts + TLS policy).
5. Return `HttpResponse` over gRPC.

Related server handler: `internal/server/handlers_replay.go`.

## Breakpoint integration

Breakpoints are applied in proxy handlers before and after upstream round trip:

- request phase: `engine.PauseRequest(tx)`
- response phase: `engine.PauseResponse(tx, statusCode, body)`

Decisions originate from `internal/breakpoints/manager.go` and are controlled by gRPC `WatchPausedRequests` + `ResumeRequest`.

Because pause decisions happen before `StoreTransaction`, persisted history reflects the final forwarded/synthetic response behavior.

## Body storage behavior and limits

- Request/response bodies are stored inline as BLOBs in `requests.body` and `responses.body`.
- Proxy enforces max body size (`MaxBodySizeMB`) before persistence.
- Oversized bodies are rejected in proxy path (413), preventing unbounded DB growth per transaction.

There is no out-of-band blob store today; all persisted payloads live in SQLite.

## Operational considerations

1. **Pruning:** retention/prune helpers live in `internal/storage/prune.go`.
2. **VACUUM:** periodic vacuum support exists in `sqlite.go` (`StartPeriodicVacuum`).
3. **Atomicity:** `SaveTransaction` avoids split writes when both sides exist.
4. **Backpressure:** live subscribers are non-blocking; overflow drops only stream events, not persistence.

## Change map for contributors

- Add/modify columns: `internal/storage/schema.go` + mapper/query scans.
- Change persistence semantics: `internal/engine/engine.go` and `internal/storage/queries.go`.
- Change replay behavior/timeouts: `internal/replay/engine.go` and builders.
- Change history shape to clients: `internal/server/handlers_traffic.go` + `pkg/api/proto/apix.proto`.
