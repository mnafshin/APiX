# Getting Started — Workflow-first Guides

> **New to APiX?** Pick a workflow below and follow the steps. Each guide is self-contained and takes less than five minutes to complete.

## Prerequisites

1. **Build the engine** (one-time):

   ```sh
   go build -o apix-engine ./cmd/apix-engine/
   go build -o apix-cli   ./cmd/apix-cli/
   ```

2. **Start the engine** (HTTP proxy on `:8080`, gRPC on `:9090`):

   ```sh
   ./apix-engine
   ```

   Verify it is running:

   ```sh
   ./apix-cli status
   ```

---

## 1 — First capture

→ **[Full how-to guide](how-to/first-capture.md)**

Quick-start:

```sh
# Send a request through the proxy
curl -x http://localhost:8080 https://httpbin.org/get

# List captured traffic
./apix-cli history list
```

You should see the captured request and its response in the output.

---

## 2 — First breakpoint

→ **[Full how-to guide](how-to/first-breakpoint.md)**

Quick-start:

```sh
# Add a breakpoint matching any URL containing "httpbin"
./apix-cli breakpoints add --pattern "httpbin"

# In another terminal, send a matching request
curl -x http://localhost:8080 https://httpbin.org/get

# Watch the paused request, then forward it
./apix-cli paused watch         # shows the paused request ID
./apix-cli paused forward <id>  # forward with original request
```

---

## 3 — Replay a captured request

→ **[Full how-to guide](how-to/replay.md)**

Quick-start:

```sh
# List captured requests to find an ID
./apix-cli history list

# Replay a specific request
./apix-cli replay --id <request-id>
```

---

## 4 — Validate your configuration

```sh
./apix-cli --config-check
```

This runs all config checks (port availability, plugin paths, regex patterns) and exits 0 on success or 1 with a full error list on failure.

---

## Next steps

| Topic | Link |
|---|---|
| Complete feature reference | [FEATURES.md](FEATURES.md) |
| HTTPS interception (CA cert setup) | [proxy_mitm.md](ARCHITECTURE/proxy_mitm.md) |
| Plugin development | [extension_arch.md](ARCHITECTURE/extension_arch.md) |
| gRPC API reference | [grpc_protobuf.md](ARCHITECTURE/grpc_protobuf.md) |
| Configuration reference | [CONFIG_VALIDATION.md](CONFIG_VALIDATION.md) |
| Storage and replay internals | [storage_replay.md](ARCHITECTURE/storage_replay.md) |
