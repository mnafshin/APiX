# VS Code Extension Architecture

Summary

The VS Code extension provides the user-facing UI for APiX by connecting to the engine gRPC API. It includes tree views for traffic/breakpoints, webview panels for request editing, and an engine process manager that can start the apix-engine binary for development.

Key files
- apix-vscode/src/extension.ts — activation, command registration
- apix-vscode/src/engineProcessManager.ts — spawns/monitors engine binary
- apix-vscode/src/trafficProvider.ts — traffic tree view
- apix-vscode/src/trafficPanel.ts — webview for inspecting requests

End-to-end flow
1. Extension activates and attempts to locate or spawn apix-engine
2. Engine publishes traffic via gRPC CaptureTraffic stream
3. trafficProvider presents events in the tree; pause/resume uses WatchPausedRequests and ResumeRequest

Acceptance criteria
- New contributor can trace a proxied request from engine capture to webview representation
- Links to the TypeScript files are present
