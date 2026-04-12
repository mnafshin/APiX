# CLI Strategy, AI-ready Contracts, and MCP Integration

Overview
- The APiX CLI is implemented as a thin gRPC client on top of the engine API. This keeps the CLI focused on UX and scripting while the engine remains the source of truth.

AI-ready / machine-friendly contracts
- CLI commands that are intended for automation should support JSON/NDJSON output and stable exit codes to be easy for scripts and AI tooling to consume.

MCP (future)
- MCP-facing work should treat the engine gRPC API as the contract surface. Design decisions should favour stability and backwards compatibility.

Where to look
- cmd/apix-cli — CLI implementation
- pkg/api/proto/apix.proto — contract

Acceptance criteria
- Doc clarifies CLI->gRPC relationship and expected automation outputs
