# HTTP Proxying & TLS MITM

APiX uses a two-layer proxy:
1. `internal/proxy/http.go` accepts plain HTTP traffic and `CONNECT` tunnels.
2. `internal/proxy/https.go` performs HTTPS MITM inside hijacked `CONNECT` sockets.

This split keeps HTTP forwarding and TLS interception isolated while sharing the same engine, plugin chain, breakpoints, and observability pipeline.

## End-to-end traffic flow

```text
Client (browser/curl)
   |
   | HTTP request OR CONNECT host:443
   v
HTTPProxy (internal/proxy/http.go)
   |-- plain HTTP  ---> handleHTTP() ---------------------------+
   |-- CONNECT      ---> handleConnect()                        |
                           |                                    |
                           | if TLS handshake bytes             |
                           v                                    |
                       TLSProxy.handleBufferedConn()            |
                           | TLS server handshake (MITM cert)   |
                           | ALPN: h2 or http/1.1              |
                           v                                    |
                 handleRequest() / handleHTTP2Request()         |
                           |                                    |
                           +--> plugin OnRequest                |
                           +--> breakpoint pause request        |
                           +--> upstream RoundTrip              |
                           +--> plugin OnResponse               |
                           +--> breakpoint pause response       |
                           +--> engine.StoreTransaction() ------+--> storage + gRPC subscribers
                           +--> observeRequest() (metrics/logs)
```

## CONNECT tunnel handling

`HTTPProxy.handleConnect()` (`internal/proxy/http.go`) does the protocol switch:

1. Hijacks the HTTP connection (`http.Hijacker`).
2. Replies `HTTP/1.1 200 Connection established`.
3. Peeks first bytes to classify payload:
   - TLS ClientHello (`0x16`) → hand off to TLS MITM.
   - HTTP/2 cleartext preface (`PRI * HTTP/2.0...`) → h2c tunnel path.
   - otherwise → raw TCP tunnel path.

This lets APiX intercept HTTPS while still supporting non-TLS tunnel use cases.

## TLS MITM handshake details

TLS interception starts in `TLSProxy.handleBufferedConn()` (`internal/proxy/https.go`):

1. Extract destination hostname from `host:port`.
2. Generate/reuse leaf certificate via `p.ca.CertForHost(hostname)`.
3. Build `tls.Config` with `NextProtos: ["h2", "http/1.1"]`.
4. Perform server-side TLS handshake against the client.
5. Branch by ALPN:
   - `h2` → `handleHTTP2Conn()` / `handleHTTP2Request()`
   - default → HTTP/1.1 loop with `http.ReadRequest()`

CA and cert lifecycle lives in:
- `internal/proxy/certauthority.go`
- `internal/proxy/cert.go`

Operators must trust APiX CA cert on client machines for HTTPS interception to work.

## HTTP/1.1 vs HTTP/2 handling

Both paths enforce the same core policy and business logic:

- input bounds (`validateInboundRequest`, 431 on violation)
- body size limits (`MaxBodySizeMB`)
- plugin request/response hooks
- request and response breakpoint pauses
- transaction persistence and metrics/access logging

Main differences:

- **HTTP/1.1 path** (`handleRequest`) writes raw HTTP responses back to `net.Conn`.
- **HTTP/2 path** (`handleHTTP2Request`) writes via `http.ResponseWriter`.
- HTTP/1.1 path supports pipelined request loop on the same TLS connection.

## Plugin hook points

Plugin execution is explicit and ordered:

1. Build `plugins.ProxyRequest`
2. `runPluginRequest(...)`
3. Optional short-circuit via `MockedResponse`
4. Upstream request/response round trip
5. `runPluginResponse(...)`

Relevant files:
- `internal/proxy/http.go` (plain HTTP path)
- `internal/proxy/https.go` (TLS paths)
- `internal/proxy/plugins.go` (shared hook wrappers)

## Breakpoint orchestration in proxy path

Before forwarding upstream, proxy calls engine pause APIs:

- `PauseRequest(tx)` may return:
  - forward unchanged/modified
  - drop (`502`)
  - synthetic response

After upstream response and plugin response hooks:

- `PauseResponse(tx, statusCode, body)` supports the same decisions.

Breakpoint control-plane comes from gRPC:
- `WatchPausedRequests`
- `ResumeRequest`

## Persistence + observability handoff

Once finalized, proxy writes one `Transaction` to engine:

- `engine.StoreTransaction(tx)` persists request/response.
- `observeRequest(...)` emits metrics, slowlog, and access-log fields.

This guarantees history and telemetry reflect post-plugin/post-breakpoint outcomes.

## Files to modify by concern

- Protocol entry/tunnel behavior: `internal/proxy/http.go`
- TLS interception / ALPN handling: `internal/proxy/https.go`
- CA and certificate generation: `internal/proxy/certauthority.go`, `internal/proxy/cert.go`
- Shared plugin execution helpers: `internal/proxy/plugins.go`
- Input/body guardrails: `internal/proxy/request_limits.go`, `internal/http/limit.go`

## Contributor checklist

When changing proxy behavior:

1. Verify both HTTP and HTTPS paths.
2. Verify HTTP/1.1 and HTTP/2 TLS branches.
3. Confirm plugin and breakpoint hooks still run in order.
4. Confirm transaction persistence fields remain correct.
5. Confirm metrics/access logs still receive request/response metadata.
