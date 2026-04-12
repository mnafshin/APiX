# Getting Started — Workflow-first Guides

This guide provides short, task-oriented workflows to get APiX users started quickly.

## First capture (quick)
1. Build and run the engine: ./apix-engine
2. Configure your tool to use the proxy: curl -x http://localhost:8080 https://example.com
3. Watch the CLI or VS Code extension for captured requests
4. Inspect the captured request and response in the UI

## First breakpoint (quick)
1. Open the VS Code extension or apix-cli and add a breakpoint that matches example.com
2. Send a request that matches the breakpoint
3. The request will pause; edit the request/response and Resume (Forward/Respond/Drop)

## Replay a captured request
1. From the traffic view, select a captured request
2. Choose "Replay" and optionally modify the request
3. Verify the replayed response and compare timings

Each of the above sections links to more detailed how-to pages (how-to/breakpoints.md, how-to/replay.md) when users need deeper examples.

(Planned via issue #124)