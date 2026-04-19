# APiX — Design Principles

This document explains the software design principles APiX follows, **why** each one matters for this specific project, the **current compliance status**, and what is being tracked for improvement.

---

## Why we need explicit principles

APiX sits at a tricky intersection:

- A **networking layer** (HTTP/HTTPS MITM proxy, WebSocket relay) that must be correct, fast, and low-latency.
- A **persistence layer** (SQLite) that must survive restarts and support rich filtering.
- A **gRPC API layer** that is shared by a VS Code extension, a CLI, and future integrations.
- A **plugin runtime** that must safely extend behavior without destabilising the core.

Without explicit principles, these layers collapse into each other. The principles below are chosen specifically because they prevent the failure modes that affect this architecture.

---

## Principles

### 1. SRP — Single Responsibility Principle

> Each module, struct, or function has exactly one reason to change.

**Why it matters for APiX:**  
The proxy, engine, gRPC server, and storage each evolve for different reasons (protocol changes, storage schema changes, API contract changes, plugin API changes). Keeping them independent means a breaking change in one layer does not cascade through the entire codebase.

**Where it is applied today:**
- `internal/engine/` — orchestrates traffic flow only; does not own HTTP or gRPC types.
- `internal/storage/` — owns persistence; does not know about proxy or gRPC.
- `internal/breakpoints/` — owns pause/resume logic only.
- `internal/replay/` — owns request replay only.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| `internal/server/grpc.go` — `EngineServer` | 580-line struct handles 8+ concerns | [#200] |
| `internal/proxy/http.go` — `handleHTTP()` | 219-line method with 11 responsibilities | [#201] |
| `internal/storage/queries.go` — `DB` | Mixes query execution, row scanning, mapping, and filter logic | [#202] |

---

### 2. SOLID — Open/Closed Principle

> A module should be open for extension but closed for modification.

**Why it matters for APiX:**  
New resume actions, replay sources, and plugin types are expected over the project's lifetime. Hard-coding type switches means every new variant requires touching existing, already-tested code.

**Where it is applied today:**
- The plugin system is architected for extension: new plugins are registered without changing the runtime.
- `apix.proto` is versioned and additive.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| `grpc.go:225-234` — `switch req.Action` | Adding a new resume action requires editing the handler | [#203] |
| `replay/engine.go:103-157` — source `switch` | Adding a new replay source (e.g. HAR) requires editing the engine | [#204] |

---

### 3. SOLID — Liskov Substitution Principle

> Implementations must honour the full contract of the interface they satisfy.

**Why it matters for APiX:**  
The proxy and gRPC layers depend on `TrafficEngine` and `PluginChain` interfaces. If an implementation silently changes error semantics (swallows errors, ignores failures), callers cannot reason about request safety or data integrity.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| `engine.go` — `StoreTransaction` swallows DB errors | Callers in proxy expect reliable error propagation | [#205] |
| `proxy/http.go` — `RunResponse` errors silently dropped | Asymmetric vs `RunRequest` which aborts on error | [#206] |

---

### 4. SOLID — Interface Segregation Principle

> Clients should not be forced to depend on methods they do not use.

**Why it matters for APiX:**  
WebSocket relay only needs `StoreWebSocketFrame`. Breakpoint paths only need `PauseRequest`. Forcing all consumers to depend on the full `TrafficEngine` interface makes mocking harder and increases coupling.

**Where it is applied today:**
- `PluginChain` is already an interface in `proxy/types.go`, not a concrete type.
- `TrafficEngine` is an interface; the proxy does not import `internal/engine` directly.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| `proxy/types.go` — `TrafficEngine` too wide | WebSocket relay depends on `PauseRequest` it never calls | [#207] |
| `proxy/types.go` — `PluginChain` forces both methods | Plugins that only touch requests must stub `OnResponse` | [#208] |
| `server/grpc.go` — full `*engine.Engine` injected | `ListPlugins` only needs the plugin runtime | [#209] |

---

### 5. SOLID — Dependency Inversion Principle

> High-level modules must not depend on low-level modules; both should depend on abstractions.

**Why it matters for APiX:**  
The engine is the core domain. It should not know whether storage is SQLite, PostgreSQL, or an in-memory map. Today, `internal/engine/engine.go` holds `*storage.DB` directly, making it impossible to unit-test the engine without an actual SQLite file and preventing any future storage backend change.

**Where it is applied today:**
- `proxy/types.go` — proxy depends on the `TrafficEngine` interface, not a concrete engine.
- `PluginChain` is injected as an interface into the proxy structs.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| `engine.go` holds `*storage.DB` | Cannot unit-test engine without SQLite | [#210] |
| `engine.go` holds `*breakpoints.Manager` | Cannot mock breakpoints in engine tests | [#211] |
| `server/grpc.go` holds concrete `*engine.Engine` | gRPC handler reaches into engine internals via `DB()` | [#212] |

---

### 6. KISS — Keep It Simple, Stupid

> Prefer the simplest solution that correctly solves the problem.

**Why it matters for APiX:**  
Networking and proxy code is already inherently complex (TLS intercept, WebSocket upgrades, concurrent streams). Adding accidental complexity — deeply nested IIFEs, multi-level fallback chains, near-identical functions — compounds maintenance burden and introduces subtle bugs.

**Where it is applied today:**
- Configuration loading uses a single flat YAML struct; no dynamic config or reflection.
- The gRPC API uses simple request/response messages; no complex polymorphic envelopes.
- Standard library `net/http` is preferred over third-party HTTP frameworks.

**Known gaps (tracked as issues):**
| Location | Violation | Issue |
|---|---|---|
| Plugin error recovery — boolean flag + IIFE (4 levels deep) | Both proxies | [#213] |
| Header source fallback — 4-level if/else chain | `engine.go:66-82` | [#214] |
| Two near-identical WebSocket header functions | `websocket.go:85-113` | [#215] |
| Metrics + slow-log recording duplicated verbatim | `http.go` and `https.go` | [#216] |

---

### 7. DRY — Don't Repeat Yourself

> Every piece of knowledge must have a single, authoritative representation in the system.

**Why it matters for APiX:**  
Header conversion, body reading, and validation logic are foundational operations used across every layer. When these are duplicated, bugs in one copy are silently absent from others (e.g. a security fix to body size limiting only applied in the HTTP proxy, not the HTTPS proxy).

**Where it is applied today:**
- `internal/httputil/` package provides shared header canonicalisation and validation helpers (`CanonicalHeader`, `IsValidHeaderValue`).
- `internal/logging/` provides a single structured logger used project-wide.

**Known gaps (tracked as issues):**
| Pattern | Occurrences | Issue |
|---|---|---|
| `io.ReadAll` + body size limiting | 20+ across proxy, replay, gRPC | [#217] |
| `make(map[string]string)` + header copy loop | 6+ in `grpc.go` alone | [#218] |
| Header validation loop (`CanonicalHeader` + `IsValidHeaderValue`) | 5+ across `grpc.go` and `replay/engine.go` | [#219] |

---

### 8. Separation of Concerns

> Each layer owns a single well-defined concern; concerns do not leak across layer boundaries.

**Why it matters for APiX:**  
The layered architecture (proxy → engine → storage) is what allows each layer to be tested, replaced, or deployed independently. When the gRPC handler calls `s.engine.DB()` directly to persist breakpoints, the storage schema becomes a concern of the API layer — breaking the boundary.

**Layer responsibilities:**

| Layer | Package | Responsibility |
|---|---|---|
| Transport | `internal/proxy/` | Intercept and relay HTTP/HTTPS; detect WebSocket upgrades |
| Orchestration | `internal/engine/` | Store transactions, evaluate breakpoints, publish to subscribers |
| Persistence | `internal/storage/` | SQLite queries, schema, migrations |
| API | `internal/server/` | Translate between gRPC proto messages and domain types |
| Extension | `internal/pluginrt/` | Safe, sandboxed request/response modification |
| Config | `internal/config/` | Load and validate YAML configuration |

**Known gaps (tracked as issues):**
| Location | Violation |
|---|---|
| `grpc.go` — `SetBreakpoint` and `ImportHAR` call `s.engine.DB()` directly | gRPC handler owns persistence logic |
| `grpc.go` — `ResumeRequest` manually constructs `http.Request` from proto | Request construction belongs in a mapper, not a handler |
| `grpc.go` — `GetHistory` builds proto messages from raw storage records inline | Conversion logic belongs in a dedicated mapper |
| `engine.go` — `StoreTransaction` handles header conversion + persistence + pub/sub in one method | Mixes three concerns in one call |

---

### 9. Hexagonal Architecture (Ports & Adapters)

> The application core is isolated from all I/O. I/O adapters implement narrow interfaces defined by the core.

**Why it matters for APiX:**  
The engine is the heart of APiX. It must be testable in isolation and independent of whether storage is SQLite or Postgres, whether the API is gRPC or REST, and whether the proxy is HTTP or QUIC. Today, the engine directly imports `net/http`, `storage`, and the gRPC generated types — violating this isolation.

**Target architecture:**
```
[HTTP Proxy adapter]   [TLS Proxy adapter]
         ↓                      ↓
   [TrafficEngine port]  [RequestPauser port]
              ↓
         [Engine core]
              ↓
   [TransactionRepository port]  [BreakpointEvaluator port]
         ↓                              ↓
 [SQLite adapter]              [Breakpoints adapter]
```

**Known gaps:**
| Location | Violation |
|---|---|
| `engine.go` imports `net/http`, `storage`, `apix` (gRPC) directly | Core depends on I/O adapters |
| `engine.go:244` — `func (e *Engine) DB() *storage.DB` | Exposes concrete adapter through a public getter |
| Engine struct holds `*storage.DB`, `*breakpoints.Manager`, `*pluginrt.Runtime` (all concrete) | No ports defined |

---

### 10. Plugin / Microkernel

> A small, stable core with well-defined extension points. Plugins extend without modifying core.

**Why it matters for APiX:**  
The plugin system is a first-class feature. Users should be able to add header editors, mock responders, rate limiters, and auth injectors without touching the proxy or engine code. This requires a narrow, stable plugin API and safe load/unload semantics.

**Where it is applied today:**
- `pkg/plugins/` defines the public plugin API: `Name()`, `Version()`, `Description()`, `OnRequest()`, `OnResponse()`.
- `internal/pluginrt/` isolates plugin execution with `recover()` guards so panicking plugins cannot crash the proxy.
- Three built-in plugins ship out of the box: `HeaderEditor`, `MockResponse`, `EnvSubst`.

**Known gaps (tracked as issues):**
| Gap | Detail |
|---|---|
| **Plugins are silent no-ops in production** | `SetPlugins()` is never called on either proxy in `cmd/apix-engine/main.go` — critical bug | [#see P0 bug] |
| Plugin `Unregister()` has no in-flight request tracking | Live unload during an active request races the plugin map |

---

### 11. YAGNI — You Aren't Gonna Need It

> Do not add abstraction or infrastructure for requirements that do not yet exist.

**Why it matters for APiX:**  
APiX is a focused developer tool. Premature abstractions add complexity to read, test, and maintain — with no current benefit. Examples to watch for: interfaces with a single implementation, configuration structs duplicated for hypothetical future backends, and cleanup code that duplicates what the runtime already handles.

**Known gaps (tracked as issues):**
| Location | Violation |
|---|---|
| `engine.go` — `DB()`, `BreakpointManager()`, `PluginRuntime()` public getters | Expose internals only because gRPC handler bypasses the engine API |
| `replay/engine.go` — `ClientConfig` duplicates `proxy.TransportOptions` fields exactly | Two structs, same defaults, no reason to differ |
| `httpProxy.Close()` / `tlsProxy.Close()` called after graceful shutdown | Server already closed connections on `Shutdown()` |

---

## Automated enforcement

Some of these principles can be checked automatically. The following tools are being evaluated for inclusion in `make lint`:

| Principle | Tool | What it checks |
|---|---|---|
| SRP / KISS | `gocyclo -over 10 ./...` | Functions with cyclomatic complexity > 10 |
| DRY | `dupl -threshold 50 ./...` | Duplicate code blocks ≥ 50 tokens |
| OCP | `exhaustive ./...` | `switch` statements missing enum cases |
| Hexagonal / SoC | `depguard` rules | Prevents `internal/engine` from importing `net/http` or gRPC packages |
| DIP | `go test ./internal/engine/...` | Engine unit tests (once `TransactionRepository` interface exists) |
| Plugin wiring | Integration test in `tests/integration/` | Verifies plugins execute on live requests |

See [testing_strategy.md](../testing/testing_strategy.md) for the full testing philosophy.

---

## Contributing

When opening a PR that touches the engine, proxy, or gRPC server, please check:

1. **Does the change add a new responsibility to an existing struct?** → Consider a new package or method group.
2. **Is the same code block copied from another file?** → Extract a shared utility in `internal/httputil/` or `internal/logging/`.
3. **Does the change reach across a layer boundary?** → e.g. proxy calling storage directly, or gRPC handler calling `engine.DB()`.
4. **Does the change add an abstraction with only one implementation?** → Only add interfaces when you need to mock, swap, or test in isolation.

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for the full development workflow.
