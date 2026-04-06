# APiX — API Debugger for VS Code

Intercept, inspect, and debug HTTP/HTTPS traffic directly in VS Code — powered by a Go MITM proxy engine.

## Features

### Traffic Inspector

A live sidebar view of all HTTP/HTTPS requests passing through the APiX proxy. Each entry shows method, URL, status code, and duration. Click any entry to open the full request and response details in a panel.

> 📸 _Traffic Inspector panel showing live request list_

### URL Breakpoints

Set a URL pattern (regex or glob) and APiX will pause matching requests before forwarding them. You can inspect the request, edit headers or the body, then choose to:

- **Forward** — send the (optionally modified) request upstream
- **Drop** — abort the request; client receives a 502
- **Respond** — return a fully synthetic response without hitting upstream

> 📸 _Breakpoints view with a paused request open in the editor_

### Request Replay

Re-send any captured request from the Traffic view with optional header and body overrides. Useful for reproducing bugs or testing API changes without re-running client code.

> 📸 _Replay panel with header override fields_

### Plugin System

Extend the proxy with built-in or custom plugins:

| Plugin | What it does |
|--------|-------------|
| **HeaderEditor** | Add, modify, or remove request/response headers |
| **MockResponse** | Return a synthetic response without hitting upstream |
| **EnvSubst** | Replace `${VAR}` placeholders in request bodies with env values |

Custom plugins implement a simple Go interface — see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Getting Started

### Install from Marketplace

Search for **APiX** in the VS Code Extensions view or run:

```
ext install apix.apix
```

### Build from Source

```bash
git clone https://github.com/mnafshin/apix.git
cd apix
make build        # build the Go engine
make ext-build    # compile the extension
make ext-package  # create .vsix
make ext-install  # install into VS Code
```

On first launch, APiX starts the engine automatically and begins capturing traffic on `http://localhost:8080`.

## Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `apix.engine.host` | `localhost` | Hostname of the APiX engine gRPC server |
| `apix.engine.grpcPort` | `9090` | gRPC port of the engine |
| `apix.engine.proxyPort` | `8080` | HTTP proxy port |
| `apix.engine.autoStart` | `true` | Start the engine automatically when VS Code opens |
| `apix.engine.tlsEnabled` | `false` | Use TLS for the gRPC connection (required for vscode.dev) |
| `apix.engine.authToken` | `""` | Bearer token for authenticating with a remote engine |
| `apix.engine.binaryPath` | `""` | Path to the `apix-engine` binary (leave empty to use bundled) |
| `apix.traffic.maxItems` | `500` | Maximum traffic items shown in the Traffic view |

## Commands

| Command | Description |
|---------|-------------|
| `APiX: Start Engine` | Start the APiX proxy engine |
| `APiX: Stop Engine` | Stop the engine |
| `APiX: Clear Traffic History` | Delete all captured traffic from storage |
| `APiX: Add URL Breakpoint` | Register a new URL breakpoint pattern |
| `APiX: Delete Breakpoint` | Remove a breakpoint |
| `APiX: Enable/Disable Breakpoint` | Toggle a breakpoint on or off |
| `APiX: Replay Request` | Re-send a captured request with optional overrides |
| `APiX: Open Traffic Inspector` | Open the traffic panel |
| `APiX: Refresh Traffic` | Manually refresh the traffic list |

## Remote Engine (vscode.dev)

APiX can connect to an engine running on a remote server, which is required when using VS Code in the browser at [vscode.dev](https://vscode.dev).

1. Deploy the `apix-engine` binary to your server
2. Configure TLS and an auth token in `config.yaml` on the server
3. In VS Code settings, set:
   - `apix.engine.host` → your server hostname
   - `apix.engine.tlsEnabled` → `true`
   - `apix.engine.authToken` → your token
4. Open VS Code at [vscode.dev](https://vscode.dev), install the extension, and connect

## Requirements

- VS Code `^1.85.0`
- **Go 1.25+** — required only if building the engine from source

## License

Apache 2.0
