# Glossary

A short glossary of terms used across APiX docs.

- MITM — Man-in-the-Middle: an intercepting proxy that can observe and modify HTTP(S) traffic.
- Breakpoint — A rule that causes a matched request to be paused for manual inspection or modification.
- Replay — Resending a captured request (optionally modified) to observe responses.
- gRPC streaming — A long-lived gRPC stream used for delivering captured traffic or paused items.
- MCP — (Planned) Management / Control Plane or remote engine surface for connecting remote APiX engines.
- Plugin — A runtime extension that can inspect, mutate, or respond to traffic programmatically.
- Slowlog — A warning emitted when a request takes longer than configured threshold.
- Probe / Health — Liveness or readiness endpoints used by orchestration systems to check service health.

(Planned via issue #127)