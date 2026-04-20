# VS Code Extension Architecture

The extension is the primary desktop UX for APiX. It manages engine process lifecycle, renders traffic/breakpoint/mocks views, and bridges gRPC streams into tree views and webviews.

## Activation and startup flow

Entry point: `apix-vscode/src/extension.ts` (`activate`).

Startup sequence:

1. Read workspace settings (`apix.engine.*`).
2. Create status bar and output channels.
3. Initialize:
   - `EngineProcessManager`
   - `EngineClient`
   - tree providers (`TrafficProvider`, `BreakpointsProvider`, `MocksProvider`)
4. Register commands (`apix.*`).
5. Auto-start engine if configured (`engine.autoStart`).

`startEngine(...)` orchestrates process startup, health check, provider refresh, and paused-request stream initialization.

## Component map

```text
extension.ts
  ├─ EngineProcessManager (spawn/restart/stop apix-engine)
  ├─ EngineClient (typed gRPC wrapper)
  ├─ TrafficProvider (tree view)
  ├─ BreakpointsProvider (tree view)
  ├─ MocksProvider (tree view)
  ├─ TrafficPanel (live webview stream)
  ├─ RequestEditor (paused request webview editor)
  └─ ReplayPanel (replay/compose webview)
```

## Engine process lifecycle

`apix-vscode/src/engineProcessManager.ts`:

- resolves binary path (workspace root override or bundled binary)
- spawns child process and watches stdout/stderr
- detects readiness (`gRPC server listening` log markers)
- handles unexpected exits
- performs bounded exponential backoff restarts
- exposes callbacks:
  - `onUnexpectedExit`
  - `onRestarting`
  - `onRestart`

On restart, `extension.ts` reinitializes providers and streams.

## gRPC client integration

`apix-vscode/src/engineClient.ts` wraps RPCs in Promise/callback-friendly methods:

- unary calls for status/history/breakpoints/replay/rules
- stream adapters for:
  - `captureTraffic(...)`
  - `watchPausedRequests(...)`

It also maps proto field names to TS interfaces (camelCase via proto-loader settings).

## Data flows

### Traffic tree (`TrafficProvider`)

`TrafficProvider.getChildren()` calls `getHistory()` and maps transactions to `TrafficItem`.

`TrafficItem` adds:
- method/url summary
- status/duration info
- request ID tooltip
- context value for command menus

### Live traffic panel (`TrafficPanel`)

Flow:

1. Webview panel opens via command `apix.openTrafficPanel`.
2. Panel starts `captureTraffic` stream.
3. Each event is posted to webview via `postMessage({ type: 'transaction', ... })`.
4. UI renders rows and detail pane in webview script.
5. Panel retries stream on errors with backoff.

### Breakpoint pause flow (`RequestEditor`)

Flow:

1. Extension subscribes to `watchPausedRequests`.
2. On paused event, `RequestEditor.showEditor(...)` opens editor webview.
3. User chooses forward/drop/respond action.
4. Editor sends action payload to extension host.
5. Host calls `ResumeRequest` through `EngineClient`.

## Webview messaging model

Webview panels use a simple message protocol:

- host -> webview:
  - `transaction`
  - `streamError`
  - `streamRecovered`
  - `websocketFrames`
- webview -> host:
  - `replay`
  - `addBreakpoint`
  - `copyAsCurl`
  - `copyRequestId`
  - `loadWebSocketFrames`

Message handling is centralized in `TrafficPanel._handleWebviewMessage(...)`.

## Command surface (selected)

Registered in `extension.ts`:

- Engine lifecycle: `apix.startEngine`, `apix.stopEngine`
- Traffic actions: `apix.openTrafficPanel`, `apix.refreshTraffic`, `apix.copyAsCurl`, `apix.copyRequestId`
- Breakpoints: add/delete/toggle + refresh
- Replay: `apix.replayRequest`, `apix.composeRequest`
- HAR import/export
- Mock/rewrite rule actions

Commands are intentionally thin wrappers around provider/client helpers.

## Reliability patterns

1. **Stream retries with backoff**
   - `watchPausedRequests` and `captureTraffic` both auto-retry.
2. **Provider refresh on reconnect**
   - process manager restart callback triggers re-sync.
3. **UI state signaling**
   - status bar switches between stopped/starting/running/disconnected.
4. **Defensive error surfacing**
   - failures shown via output channel and VS Code notifications.

## File responsibility map

- `src/extension.ts` — composition root: activation, commands, stream setup, status bar updates.
- `src/engineProcessManager.ts` — child-process lifecycle and restart strategy.
- `src/engineClient.ts` — gRPC transport wrappers and type mapping.
- `src/trafficProvider.ts` — tree-view model for historical transactions.
- `src/breakpointsProvider.ts` — tree-view model for breakpoint rules.
- `src/mocksProvider.ts` — tree-view model for rewrite/mock rules.
- `src/trafficPanel.ts` — live capture webview, postMessage routing.
- `src/requestEditor.ts` — paused request edit-and-resume webview.
- `src/replayPanel.ts` — replay/compose UI and template management.

## Adding a new UI feature safely

1. Add/extend RPC in proto + engine server.
2. Implement client method in `engineClient.ts`.
3. Add provider/panel wiring in `extension.ts`.
4. Add command + menu contribution in `package.json` if user-triggered.
5. Ensure reconnect/stream retry behavior still works.
