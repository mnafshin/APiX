# Storage, Replay, and Breakpoint Orchestration

Why storage exists
- APiX persists captured transactions so users can inspect, replay, and debug historical traffic.
- SQLite is used for simplicity, portability, and transactional guarantees.

Where to look
- internal/storage — SQL schema and queries
- internal/engine — in-memory store, pub/sub to gRPC streams
- internal/replay — replay engine that re-sends recorded requests
- internal/breakpoints — matching, pause/resume orchestration

Important notes
- SQLite PRAGMAs are tuned for performance in production; tests use an ephemeral DB.
- Replay respects original request headers and supports modifications via the extension's RequestEditor.

Acceptance criteria
- Doc explains the flow between storage, engine, and replay
- Links to the relevant files are included
