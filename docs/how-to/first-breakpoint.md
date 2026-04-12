# How-to: First breakpoint

A breakpoint pauses a matching HTTP request before it reaches the upstream server. You can inspect it, modify headers or the body, and then choose to **Forward** the request, **Drop** it (return an error), or **Respond** with a synthetic response — all without changing the client or server code.

## Prerequisites

- Engine running (`./apix-engine`)
- At least one request captured previously (see [first-capture.md](first-capture.md))

## Step 1 — Add a breakpoint

```sh
# Pause any request whose URL contains "httpbin"
./apix-cli breakpoints add --pattern "httpbin"
```

List breakpoints to confirm:

```sh
./apix-cli breakpoints list
```

Output:

```
ID                                    PATTERN   ENABLED
a1b2c3d4-...                          httpbin   true
```

> **Pattern syntax:** The pattern is a Go regular expression (`regexp.Compile`). Examples:
> - `httpbin` — matches any URL containing "httpbin"
> - `^https://api\.example\.com/v2/` — matches a specific API prefix
> - `\.(png|jpg|gif)$` — matches image requests

## Step 2 — Watch for paused requests (in a second terminal)

```sh
./apix-cli paused watch
```

This command blocks and streams paused requests as they arrive.

## Step 3 — Trigger the breakpoint

In a third terminal, send a matching request through the proxy:

```sh
curl -x http://localhost:8080 https://httpbin.org/get --insecure
```

The `paused watch` terminal prints something like:

```
REQUEST_ID: 7f3a9b12-...
METHOD: GET
URL: https://httpbin.org/get
HEADERS:
  User-Agent: curl/7.88.1
  Accept: */*
```

The curl command is still **hanging** — the request is paused until you decide what to do.

## Step 4 — Decide: Forward, Drop, or Respond

### Option A — Forward (pass the original request through)

```sh
./apix-cli paused forward <REQUEST_ID>
```

The request is forwarded to the upstream unchanged. curl receives the real response.

### Option B — Drop (return an error to the client)

```sh
./apix-cli paused drop <REQUEST_ID>
```

The request is dropped and curl sees a connection reset.

### Option C — Respond with a synthetic response

```sh
./apix-cli paused respond <REQUEST_ID> \
  --status 200 \
  --header "Content-Type: application/json" \
  --body '{"intercepted": true}'
```

curl receives your crafted response instead of hitting the real server.

## Step 5 — Disable or remove the breakpoint

```sh
# Temporarily disable without deleting
./apix-cli breakpoints disable <BREAKPOINT_ID>

# Re-enable
./apix-cli breakpoints enable <BREAKPOINT_ID>

# Delete permanently
./apix-cli breakpoints delete <BREAKPOINT_ID>
```

## Using breakpoints from the VS Code extension

1. Open the APiX sidebar in VS Code.
2. Click **Breakpoints → Add Breakpoint**.
3. Enter the URL pattern and click **Save**.
4. Send a matching request — a notification appears and the **Paused Requests** view shows the entry.
5. Click **Forward**, **Drop**, or **Respond** directly in the extension.

## Tips

- Multiple breakpoints are evaluated in insertion order; the **first** match wins.
- Disable all breakpoints at once with `./apix-cli breakpoints disable --all`.
- Breakpoints survive engine restart as long as they are re-registered via the API or extension.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `paused watch` shows nothing | Make sure the breakpoint is enabled and the request URL matches the pattern. |
| curl hangs forever | The breakpoint is still paused — run `forward`, `drop`, or `respond`. |
| Pattern doesn't match | Test your regex with `go run` or an online Go regex tester. |

## Next steps

- **[Replay](replay.md)** — re-send a captured request with optional modifications.
- **[First capture](first-capture.md)** — back to basics if needed.
