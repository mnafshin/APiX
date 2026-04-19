# How-to: Replay a captured request

Replay lets you re-send any previously captured HTTP request, optionally with modifications, and see the new response side-by-side. It is useful for debugging, regression testing, and exploring how an API behaves under different conditions.

## Prerequisites

- Engine running (`./apix-engine`)
- At least one request in the capture history (see [first-capture.md](first-capture.md))

## Step 1 — Find the request ID

```sh
./apix-cli history list
```

Output:

```
ID                                    METHOD  URL                             STATUS  DURATION
3e7c1a2b-f10a-4c8e-b3a7-9d0e52c14f88  GET     https://httpbin.org/get         200     142ms
8a1c3d5e-...                          POST    https://httpbin.org/post        201      89ms
```

Copy the ID of the request you want to replay.

## Step 2 — Replay without modifications

```sh
./apix-cli replay --id 3e7c1a2b-f10a-4c8e-b3a7-9d0e52c14f88
```

APiX re-sends the exact original request and prints the new response:

```
STATUS: 200 OK
DURATION: 128ms
HEADERS:
  Content-Type: application/json
BODY:
{
  "url": "https://httpbin.org/get",
  ...
}
```

## Step 3 — Replay with header modifications

Add, replace, or remove headers with `--header` (repeatable):

```sh
./apix-cli replay --id <ID> \
  --header "Authorization: Bearer my-test-token" \
  --header "X-Debug: true"
```

## Step 4 — Replay with a different body

Replace the request body entirely:

```sh
./apix-cli replay --id <ID> \
  --body '{"key": "new-value"}'
```

Combined with a header change:

```sh
./apix-cli replay --id <ID> \
  --header "Content-Type: application/json" \
  --body '{"user": "alice"}'
```

## Step 5 — Replay against a different host

Use `--target` to send the request to a different base URL while keeping the original path:

```sh
./apix-cli replay --id <ID> --target https://staging.example.com
```

This is useful for comparing production and staging responses.

## Step 6 — Compare original vs replayed

Use `--compare` to print a diff of the original and new response bodies:

```sh
./apix-cli replay --id <ID> --compare
```

Output:

```diff
  {
-   "origin": "10.0.0.1",
+   "origin": "10.0.0.2",
    "url": "https://httpbin.org/get"
  }
```

## Using replay from the VS Code extension

1. In the **Traffic** tree view, right-click any captured request.
2. Select **Replay Request**.
3. The **Replay** panel opens pre-filled with the original request.
4. Edit headers or body if needed, then click **Send**.
5. The response appears in the right-hand pane alongside the original.

## Tips

- Replay honours the `replay_skip_tls_verify` config option for HTTPS targets.
- The replayed request and its response are stored as a new capture in history.
- Use `--count N` to replay the same request N times in sequence (load-testing).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `request not found` | The ID does not exist — run `history list` to get a valid one. |
| Replay returns a different status than original | Normal — the server state may have changed. Use `--compare` to see what changed. |
| TLS error during replay | Add `replay_skip_tls_verify: true` in `config.yaml` or use `--skip-tls`. |

## Next steps

- **[First breakpoint](first-breakpoint.md)** — pause and edit requests before they hit the server.
- **[gRPC API reference](../ARCHITECTURE/grpc_protobuf.md)** — drive replay programmatically.
