# How-to: First capture

This guide walks you through capturing your first HTTP/HTTPS request with APiX. You will go from a fresh build to seeing full request and response details in under five minutes.

## What you will need

- Go 1.21+ installed
- `curl` (or any HTTP client)
- APiX source code (or pre-built binaries)

## Step 1 — Build and start the engine

```sh
# Build (skip if you already have binaries)
go build -o apix-engine ./cmd/apix-engine/
go build -o apix-cli   ./cmd/apix-cli/

# Start the engine (HTTP proxy :8080 · gRPC :9090)
./apix-engine
```

You should see:

```
APiX engine listening on :8080 (proxy) and :9090 (gRPC)
```

> **Tip:** The engine stores captures in `apix.db` in the current directory by default. Use `--config` to point it at a different config file.

## Step 2 — Send traffic through the proxy

Open a second terminal and send any HTTP or HTTPS request through the proxy:

```sh
# Plain HTTP
curl -x http://localhost:8080 http://httpbin.org/get

# HTTPS (you will see a TLS warning until you install the APiX CA — see proxy_mitm.md)
curl -x http://localhost:8080 https://httpbin.org/get --insecure
```

The proxy logs the request immediately.

## Step 3 — List captured traffic

```sh
./apix-cli history list
```

Sample output:

```
ID                                    METHOD  URL                        STATUS  DURATION
3e7c1a2b-...                          GET     https://httpbin.org/get    200     142ms
```

## Step 4 — Inspect a specific request

```sh
./apix-cli history get <ID>
```

This prints the full request headers, body, response status, response headers, and body.

## Step 5 — Watch traffic live

Instead of listing after the fact, open a live stream:

```sh
./apix-cli watch
```

Every new request appears in the terminal as it is captured.

## What just happened?

```
curl  ──► APiX proxy (:8080) ──► real upstream
                │
                ▼
          APiX engine stores request + response in SQLite
                │
                ▼
          apix-cli reads from engine over gRPC (:9090)
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `curl: (7) Failed to connect to localhost port 8080` | Engine is not running. Start `./apix-engine` first. |
| HTTPS request fails with `SSL: no alternative certificate subject name matches target host name` | Install the APiX CA certificate. See [proxy_mitm.md](../proxy_mitm.md). |
| `history list` shows nothing | Make sure curl is proxied (`-x http://localhost:8080`) and try again. |

## Next steps

- **[First breakpoint](first-breakpoint.md)** — pause a request before it reaches the server and edit it in place.
- **[Replay](replay.md)** — re-send a captured request, optionally with modifications.
- **[HTTPS interception](../proxy_mitm.md)** — install the CA cert to capture HTTPS traffic without warnings.
