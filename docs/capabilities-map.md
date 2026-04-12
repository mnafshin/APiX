# APiX Capabilities Map

This map shows shipped vs planned capabilities and the primary surface (engine, VS Code, CLI).

| Capability | Status | Surfaces | Notes / links |
|---|---:|---|---|
| HTTP/HTTPS intercepting proxy | Shipped | Engine | core proxy: internal/proxy |
| gRPC streaming API | Shipped | Engine, CLI, VS Code | pkg/api/proto/apix.proto |
| Breakpoints (pause/resume) | Shipped | Engine, VS Code | docs/how-to/breakpoints.md |
| Replay requests | Shipped | Engine, CLI, VS Code | internal/replay |
| Plugins runtime | Shipped (limited) | Engine | plugin runtime under internal/plugins |
| Observability (metrics/slowlog) | Shipped | Engine | internal/metrics |
| Remote engine / MCP | Planned | Engine, VS Code | roadmap: issue tracker |
| Cookbook / Recipes | Planned | Docs | follow-up after docs restructure |

(Planned via issue #126)