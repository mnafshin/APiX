---
marp: false
theme: default
paginate: true
backgroundColor: #0d1117
color: #e6edf3
style: |
  section {
    font-family: 'Segoe UI', system-ui, sans-serif;
    padding: 48px 64px;
  }
  h1 { color: #58a6ff; font-size: 2.2em; margin-bottom: 0.2em; }
  h2 { color: #58a6ff; border-bottom: 2px solid #21262d; padding-bottom: 0.3em; }
  h3 { color: #79c0ff; }
  code { background: #161b22; color: #f0883e; padding: 2px 6px; border-radius: 4px; }
  pre { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; }
  pre code { color: #e6edf3; background: none; padding: 0; }
  ul li { margin: 0.4em 0; }
  strong { color: #ffa657; }
  em { color: #79c0ff; }
  table { border-collapse: collapse; width: 100%; font-size: 0.85em; }
  th { background: #21262d; color: #58a6ff; padding: 8px 12px; }
  td { padding: 8px 12px; border-top: 1px solid #21262d; }
---

<!-- _class: lead -->
# APiX — API Debugger
### Debug HTTP/HTTPS traffic without leaving VS Code

_Intercept · Inspect · Edit · Replay_

---

## What is APiX?

APiX is an **API debugging toolkit** built as a VS Code extension backed by a Go proxy engine.

- Acts as an intercepting MITM proxy for all HTTP/HTTPS traffic
- Lets you **pause, inspect, edit, and resume** requests — like a code debugger, but for APIs
- Runs entirely in VS Code (or vscode.dev for browser-based use)
- No separate UI to install — uses VS Code's own panels, tree views, and webviews

---

## Ability #1 — Traffic Inspection
- Route any HTTP/HTTPS client through `http://localhost:8080`
- Every request/response is captured in the **Traffic Inspector** panel
- Live streaming — see requests appear as they happen
- Click any item to view headers and body
- Persists to **SQLite** — history survives restarts

---

## Ability #2 — URL Breakpoints
- Set a regex pattern to match URLs (e.g. `.*api/users.*`)
- Matching requests are **paused** before being forwarded
- The **Request Editor** opens automatically — inspect headers and body
- Choose to:
  - ▶ **Forward** (optionally with edits)
  - ✕ **Drop** (returns 502 to client)
  - ↩ **Respond** (return a synthetic response without hitting upstream)

---

## Ability #3 — Request Replay
- Right-click any captured request → **Replay**
- Override headers and/or body before resending
- Response shown inline — compare against original

---

## Ability #4 — Plugin System
Three built-in plugins, plus a public SDK to write your own:

| Plugin | What it does |
|---|---|
| **HeaderEditor** | Add, remove, or replace request/response headers |
| **MockResponse** | Return a synthetic response for matched URLs |
| **EnvSubst** | Replace `{{ENV_VAR}}` placeholders in headers and body |

Custom plugins implement the `Plugin` interface from `pkg/plugins/sdk.go`.

---

## Ability #5 — Remote Engine (vscode.dev / browser)
- Run `apix-engine` on any server with TLS + auth token
- Connect the VS Code extension via `apix.engine.host` / `apix.engine.tlsEnabled`
- Full feature parity — no installation required on the client machine

---

## Architecture

```
┌────────────────────────────────────────────┐
│          VS Code Extension (TypeScript)     │
│                                             │
│  Traffic Panel  Breakpoints View  Replay   │
│        ↑               ↑            ↑       │
│        └───────── EngineClient ─────┘       │
└─────────────────────┬──────────────────────┘
                      │ gRPC (local or TLS)
┌─────────────────────▼──────────────────────┐
│              Go Engine Binary               │
│                                             │
│  HTTP/HTTPS MITM Proxy  (:8080)            │
│  Breakpoint Manager                         │
│  Plugin Runtime                             │
│  SQLite Storage                             │
│  Replay Engine                              │
│  gRPC API Server        (:9090)            │
└────────────────────────────────────────────┘
```

**12 gRPC RPCs** connect the extension to the engine:

| RPC | Type | Purpose |
|---|---|---|
| `GetStatus` | Unary | Health check |
| `CaptureTraffic` | Server-stream | Live traffic feed |
| `ListPlugins` | Unary | Plugin inventory |
| `SetBreakpoint` | Unary | Register URL pattern |
| `DeleteBreakpoint` | Unary | Remove breakpoint |
| `ListBreakpoints` | Unary | List all breakpoints |
| `WatchPausedRequests` | Server-stream | Breakpoint hit notifications |
| `ResumeRequest` | Unary | Forward / drop / respond |
| `ReplayRequest` | Unary | Replay with overrides |
| `GetHistory` | Server-stream | Paginated traffic history |
| `ClearHistory` | Unary | Wipe storage |

---

## Getting Started

### Prerequisites
- Go 1.25+
- Node.js 20+
- VS Code 1.85+

### Build & Install (5 steps)

```bash
# Clone repo
git clone https://github.com/mnafshin/apix.git
cd apix

# 1. Build the engine binary
make build

# 2. Build the VS Code extension
make ext-build

# 3. Package the extension
make ext-package

# 4. Install locally
make ext-install

# 5. Open VS Code — the APiX icon appears in the sidebar
```

### Send Traffic Through the Proxy

```bash
# Any HTTP client — just set the proxy
curl -x http://localhost:8080 https://api.example.com/users

# Or configure your app's HTTP client to use localhost:8080
# For HTTPS, trust the generated CA certificate:
#   macOS: open ~/.apix/ca.pem → trust in Keychain
#   Linux: sudo cp ~/.apix/ca.pem /usr/local/share/ca-certificates/ && update-ca-certificates
```

---

## VS Code Walkthrough

### Starting the Engine
1. Open VS Code with the APiX extension installed
2. Engine starts automatically (or: `Ctrl+Shift+P` → **APiX: Start Engine**)
3. Status bar shows `APiX: Running ●`

### Inspecting Traffic
1. Click the **APiX icon** in the activity bar
2. The **Traffic** tree view lists captured transactions
3. Click any item to open the detail webview (headers, body, status, timing)

### Setting a URL Breakpoint
1. In the **Breakpoints** tree view, click `+`
2. Enter a URL regex pattern, e.g. `.*api/login.*`
3. Choose methods (or leave blank for all)
4. Send a matching request — the **Request Editor** opens automatically

### Using the Request Editor
```
[Method]  [URL]
Headers:  key: value  (editable)
Body:     (editable textarea)

[ Forward ]  [ Drop ]  [ Respond ]
```

### Replaying a Request
1. Right-click any traffic item → **APiX: Replay Request**
2. Edit headers or body in the Replay panel
3. Click **Send** — response appears below

---

## Developer Workflow

```bash
make test                                           # Run 89 Go tests (with race detector)
make test-one TEST=TestSaveAndGetRequest PKG=./internal/storage/  # Single test
make proto                                          # Regenerate gRPC code after .proto changes
make build-all                                      # Cross-compile (macOS/Linux/Windows)
make dev                                            # Build engine + extension together
./scripts/run-e2e.sh                                # End-to-end smoke test
```

### Writing a Custom Plugin

```go
// myplugin/myplugin.go
package myplugin

import (
    "context"
    "github.com/mnafshin/apix/pkg/plugins"
)

type MyPlugin struct{}

func (p *MyPlugin) Name()        string { return "my-plugin" }
func (p *MyPlugin) Version()     string { return "1.0.0" }
func (p *MyPlugin) Description() string { return "Does something useful." }

func (p *MyPlugin) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
    clone := req.Clone(req.Body)
    clone.Headers.Set("X-My-Header", "injected")
    return clone, nil
}

func (p *MyPlugin) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
    return nil, nil // pass through
}
```

Register in `cmd/apix-engine/main.go`:
```go
pluginRT.Register(&myplugin.MyPlugin{})
```

---

## Remote / vscode.dev Setup

```bash
# Recommended: use the environment variable
APIX_AUTH_TOKEN=your-secret-token ./apix-engine
```

```jsonc
// VS Code settings.json
{
  "apix.engine.host": "your-server.example.com",
  "apix.engine.grpcPort": 9090,
  "apix.engine.tlsEnabled": true,
  "apix.engine.authToken": "your-secret-token",
  "apix.engine.autoStart": false
}
```

---

## Extension Settings

| Setting | Default | Description |
|---|---|---|
| `apix.engine.host` | `localhost` | Engine gRPC hostname |
| `apix.engine.grpcPort` | `9090` | Engine gRPC port |
| `apix.engine.proxyPort` | `8080` | HTTP proxy port |
| `apix.engine.autoStart` | `true` | Auto-launch engine on VS Code open |
| `apix.engine.tlsEnabled` | `false` | Use TLS (required for remote/vscode.dev) |
| `apix.engine.authToken` | `""` | Bearer token for remote engine |
| `apix.engine.binaryPath` | `""` | Custom engine binary path |
| `apix.traffic.maxItems` | `500` | Max items shown in Traffic view |

---

## Project Structure

```
apix/
├── cmd/apix-engine/        Entry point — wires all components
├── internal/
│   ├── proxy/              HTTP + HTTPS MITM proxy, TLS cert generation
│   ├── breakpoints/        URL pattern matching, pause/resume state machine
│   ├── pluginrt/           Plugin runtime + 3 built-in plugins
│   ├── storage/            SQLite backend (WAL mode, FK enforcement)
│   ├── replay/             Request replay with overrides
│   ├── engine/             Central coordinator (implements TrafficEngine)
│   ├── server/             gRPC server (12 RPCs)
│   └── config/             YAML configuration loader
├── pkg/
│   ├── api/proto/          Source of truth for gRPC API (.proto)
│   ├── api/generated/      Generated Go gRPC stubs
│   └── plugins/            Public Plugin SDK (importable by external plugins)
├── apix-vscode/            VS Code extension (TypeScript)
├── build/                  Dockerfile
├── scripts/run-e2e.sh      End-to-end smoke test
└── .github/workflows/      CI (build+test) + Release (cross-compile + VSIX)
```

---

## Roadmap

| Milestone | Features |
|---|---|
| **v0.1 (current)** | MITM proxy, breakpoints, replay, plugins, SQLite, VS Code extension |
| **v0.2** | WebSocket support, conditional breakpoints, plugin config UI, gRPC proxy support |
| **v1.0** | Wasm plugin sandbox (external plugins without recompile), VS Code Marketplace publish, CI/CD integration hooks |

---

<!-- _class: lead -->

## Get Started

```bash
git clone https://github.com/mnafshin/apix && cd apix
make build && make ext-install
```

Open VS Code — the **APiX panel** appears in the sidebar.

---

_Apache 2.0 · github.com/mnafshin/apix_

> **Tip:** View this deck with [Marp for VS Code](https://marketplace.visualstudio.com/items?itemName=marp-team.marp-vscode) —
> install the extension, open this file, and click the **preview** icon (top-right) for live slides.
