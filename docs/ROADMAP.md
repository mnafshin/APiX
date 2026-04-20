# APiX Roadmap

> Living document — last updated 2026-04-20

APiX is evolving from a capable HTTP/HTTPS intercepting proxy into a **top-class API debugging toolkit** and ultimately a **zero-friction developer platform**. This roadmap organizes 90+ open issues into a phased plan across seven workstreams.

---

## Overview

| Phase | Theme | Issues | Target |
|-------|-------|--------|--------|
| **v2.1** | Foundation & Stability | 28 | Near-term |
| **v2.2** | Developer Experience | 22 | Mid-term |
| **v2.3** | Power Features | 15 | Mid-term |
| **v3.0** | Platform & Differentiation | 15 | Long-term |
| **v4.0** | Zero-Friction Developer Tool | 12 | Vision |

### Workstreams

| Stream | Label | Description |
|--------|-------|-------------|
| 🔧 **Code Health** | `design-principles` | Refactors aligned with architecture principles (SRP, DRY, DIP, ISP) |
| 🔒 **Security** | `security` | Auth, sandboxing, rate limiting, input validation |
| 🎨 **UX Evolution** | `ux-evolution` | CLI and VS Code extension usability improvements |
| 📊 **Observability** | `observability` | Logging, tracing, metrics, monitoring |
| 🚀 **Features** | `enhancement` | New capabilities and integrations |
| 🧩 **Platform** | `platform` | Record, mock, guard, diff, share — workflow-level capabilities |
| 🎯 **Developer Experience** | `developer-experience` | Zero-config adoption: process wrapping, SDK hooks, project init |

---

## Phase 1 — v2.1: Foundation & Stability

*Focus: Fix bugs, plug security holes, refactor high-severity code smells, and establish observability baseline.*

### 🐛 Bug Fixes (3)

| # | Issue | Effort |
|---|-------|--------|
| #383 | Fix `context.Background()` in 12+ logging calls (loses request_id) | M |
| #374 | Fix `--output json` for streaming commands (invalid JSON) | S |
| #376 | Remove dead code (`indentXml`) in RequestEditor | S |

### 🔒 Security Hardening (5)

| # | Issue | Effort |
|---|-------|--------|
| #307 | Add input size bounds for headers/metadata (OOM prevention) | S |
| #290 | Encrypt auth token in config + file permission checks | M |
| #291 | Add rate limiting to HTTP/HTTPS proxy layer | M |
| #363 | Move auth token to VS Code SecretStorage | S |
| #292 | Add audit logging for state-changing operations | M |

### 🔧 Code Health — High Priority Refactors (8)

| # | Issue | Effort |
|---|-------|--------|
| #320 | Deduplicate TLS proxy handleRequest / handleHTTP2Request (330 LOC) | L |
| #319 | API handlers bypass Engine — route through Engine API | L |
| #317 | Engine.DB() / PluginRuntime() expose concrete types — DIP violation | M |
| #318 | Extract 6 duplicated panic recovery blocks | S |
| #321 | Extract maxBodyBytes calculation (repeated 3×) | S |
| #323 | Extract duplicated store-transaction + observe-metrics blocks (3×) | S |
| #328 | Extract 14+ responsibilities from main() | M |
| #326 | Plugin interface forces both Request + Response — split | M |

### 📊 Observability Baseline (8)

| # | Issue | Effort |
|---|-------|--------|
| #379 | Configurable log level (currently hardcoded Debug) | S |
| #380 | Add `Debugf` log helper function | S |
| #386 | Add `log_level` and `log_format` to config.yaml | S |
| #381 | Wire OTel tracing plugin in engine startup | M |
| #382 | Propagate W3C traceparent header through proxy | M |
| #384 | Add `--verbose` / `--debug` flags to CLI | M |
| #385 | Expose request_id to CLI and extension users | M |
| #390 | Engine access log (structured per-request log) | M |

### 🚀 Stability Features (4)

| # | Issue | Effort |
|---|-------|--------|
| #306 | gRPC stream errors → extension UI goes stale (reconnect) | M |
| #305 | Tree refresh throttling in extension (high-traffic) | S |
| #296 | Map Local rules — clarify status + add test coverage | M |
| #308 | Config hot-reload without engine restart | M |

**Phase 1 total: 28 issues**

---

## Phase 2 — v2.2: Developer Experience

*Focus: Make APiX delightful to use. CLI gets colors and ergonomics; extension gets unified UI and keyboard-driven workflow.*

### 🎨 CLI Ergonomics (7)

| # | Issue | Effort |
|---|-------|--------|
| #346 | Enable ANSI colors in CLI text output | M |
| #348 | Fix `--help` on all subcommands | S |
| #353 | Standardize positional args vs flags for entity IDs | M |
| #358 | Environment variable support for connection flags | S |
| #360 | Template execute command | S |
| #361 | Stdin body support for send/replay | S |
| #355 | Interactive TUI mode (`apix tui`) | L |

### 🎨 Extension UX (8)

| # | Issue | Effort |
|---|-------|--------|
| #345 | Keyboard shortcuts for common actions | S |
| #347 | Welcome/onboarding webview on first activation | M |
| #349 | Column sorting in traffic table | M |
| #350 | Filter persistence across panel reopens | S |
| #354 | Unified tabbed webview panel | L |
| #356 | Resizable detail pane in traffic panel | M |
| #357 | Inline JSON validation for header textareas | S |
| #375 | ARIA accessibility support for webviews | M |

### 📊 Observability Polish (4)

| # | Issue | Effort |
|---|-------|--------|
| #387 | Structured logging in VS Code extension output channel | M |
| #389 | Request/response size metrics | S |
| #391 | Error rate and latency percentile metrics | M |
| #393 | Log rotation / max file size configuration | M |

### 🔧 Code Health — Medium Priority (3)

| # | Issue | Effort |
|---|-------|--------|
| #325 | ServerRepository interface has 18 methods — ISP violation | M |
| #334 | BreakpointEvaluator: split matcher from manager (8 methods) | M |
| #329 | Monolithic 160-line validate() in config — split by category | S |

**Phase 2 total: 22 issues**

---

## Phase 3 — v2.3: Power Features

*Focus: Capabilities that differentiate APiX from basic proxies. Mocking, replay, security features, and rich visualization.*

### 🚀 Core Features (5)

| # | Issue | Effort |
|---|-------|--------|
| #294 | Expose breakpoint conditions in VS Code extension | M |
| #295 | Build response mocking UX (extension + CLI) | L |
| #297 | Runtime plugin enable/disable without restart | M |
| #298 | Expand GraphQL debugging capabilities | L |
| #302 | Body compression in SQLite storage | M |

### 🎨 Rich UX (5)

| # | Issue | Effort |
|---|-------|--------|
| #359 | Mock rule creation UI in extension | M |
| #362 | Request diff view (original vs modified) | M |
| #365 | Rich body viewer (JSON tree, image preview, hex dump) | L |
| #366 | Request timeline / waterfall view | L |
| #364 | Virtual scrolling for traffic table (10K+ items) | L |

### 🔒 Advanced Security (3)

| # | Issue | Effort |
|---|-------|--------|
| #289 | mTLS and OAuth/OIDC for gRPC authentication | L |
| #293 | Plugin execution sandboxing | L |
| #392 | Grafana dashboard template | M |

### 🔧 Code Health — Cleanup (2)

| # | Issue | Effort |
|---|-------|--------|
| #332 | Remove 11 dead builtin plugins (never registered) | M |
| #335 | Plugin runner type assertions — compile-time safety | M |

**Phase 3 total: 15 issues**

---

## Phase 4 — v3.0: Platform & Differentiation

*Focus: Transform APiX from a tool into a platform. Session management, scripting, AI integration, and multi-engine orchestration.*

### 🚀 Platform Features (10)

| # | Issue | Effort |
|---|-------|--------|
| #367 | AI-assisted error analysis for requests | L |
| #368 | Traffic diff mode for CLI | M |
| #369 | Session recording and playback | L |
| #370 | Multi-engine dashboard | L |
| #371 | Load testing integration | L |
| #372 | Script/macro support (`.apix` files) | L |
| #373 | Profile system for CLI (staging/prod configs) | M |

### 🔧 Code Health — Final Polish (5)

| # | Issue | Effort |
|---|-------|--------|
| #322 | Inline header map construction → use httputil | S |
| #327 | Inline proto message construction in GetHistory | S |
| #330 | Replay ClientConfig duplicates TransportOptions | S |
| #331 | Breakpoint action switch duplicated 4× | M |
| #336 | Deduplicate scanRewriteRule / scanRewriteRuleRow | S |
| #337 | Deduplicate get-with-default helper in plugins | S |
| #338 | Engine public getters bypass Engine API | S |
| #339 | Proxy plugins field typed as `any` → interface | S |

**Phase 4 total: 18 issues**

---

## Effort Legend

| Size | Meaning |
|------|---------|
| **S** | Small — a few hours, localized change |
| **M** | Medium — 1-2 days, touches multiple files |
| **L** | Large — 3+ days, cross-cutting or new subsystem |

---

## Dependency Graph

```
Phase 1 (Foundation)
  ├── #379 log level ──→ #380 Debugf ──→ #384 CLI --verbose
  ├── #381 wire OTel ──→ #382 traceparent ──→ #385 expose request_id
  ├── #317 DIP fix ──→ #319 route through Engine ──→ #338 remove public getters
  ├── #326 split Plugin iface ──→ #335 type-safe runner ──→ #339 typed plugins field
  ├── #318 panic recovery ──→ #320 deduplicate TLS handlers
  └── #307 input bounds ──→ #291 rate limiting

Phase 2 (DX)
  ├── #346 CLI colors ──→ #355 TUI mode
  ├── #354 unified webview ──→ #356 resizable pane ──→ #364 virtual scrolling
  └── #386 config log fields ──→ #393 log rotation

Phase 3 (Power)
  ├── #295 mocking UX ──→ #359 mock creation UI
  ├── #362 diff view ──→ #368 traffic diff CLI
  └── #389 size metrics ──→ #391 percentile metrics ──→ #392 Grafana dashboard

Phase 4 (Platform v3.0)
  ├── #372 scripting ──→ #371 load testing
  └── #369 session recording ──→ #370 multi-engine

Phase 5 (Zero-Friction v4.0)
  ├── #405 apix run (env injection) ──→ #416 apix system (system-wide proxy)
  ├── #405 apix run ──→ #406 apix record ──→ #407 apix mock
  ├── #406 apix record ──→ #410 apix diff
  ├── #406 apix record ──→ #413 apix share
  ├── #406 apix record ──→ #409 apix learn (OpenAPI gen)
  ├── #405 apix run ──→ #408 apix guard
  ├── #414 apix init (.apix/ project dir) ── standalone
  ├── #411 apix doctor ── uses live engine or recordings
  ├── #412 apix export ── uses live engine or recordings
  └── #415 SDK hooks (Go, Python, Node.js) ── standalone
```

---

## Phase 5 — v4.0: Zero-Friction Developer Tool

*Focus: Make APiX the tool developers reach for daily. Process wrapping, record/mock workflows, traffic guard, API diagnostics, and project-native config.*

### 🎯 Zero-Config Adoption (3)

| # | Issue | Effort |
|---|-------|--------|
| #405 | `apix run` — wrap any process with automatic proxy routing | M |
| #416 | `apix system` — system-wide proxy toggle (macOS/Linux/Windows) | M |
| #414 | `apix init` — project-native `.apix/` configuration directory | S |

### 🧩 Record & Replay Workflow (3)

| # | Issue | Effort |
|---|-------|--------|
| #406 | `apix record` — capture traffic sessions to replayable files | M |
| #407 | `apix mock` — serve recorded traffic as a mock API server | L |
| #413 | `apix share` — collaborative session sharing | M |

### 🧩 Analysis & Intelligence (3)

| # | Issue | Effort |
|---|-------|--------|
| #409 | `apix learn` — auto-generate OpenAPI spec from traffic | L |
| #410 | `apix diff` — detect API changes between sessions | L |
| #411 | `apix doctor` — API usage health diagnostics | L |

### 🧩 Integration & Export (3)

| # | Issue | Effort |
|---|-------|--------|
| #408 | `apix guard` — traffic allowlist enforcement for CI/CD | M |
| #412 | `apix export` — generate code/collections from traffic | M |
| #415 | Language-native SDK hooks (Go, Python, Node.js) | L |

**Phase 5 total: 12 issues**

---

## Success Metrics

| Metric | Current | v2.1 Target | v3.0 Target | v4.0 Target |
|--------|---------|-------------|-------------|-------------|
| Test coverage | 71.5% | 80% | 90% | 90%+ |
| Open bugs | 3 | 0 | 0 | 0 |
| Security issues | 7 | 2 | 0 | 0 |
| Code smells (HIGH) | 5 | 0 | 0 | 0 |
| CLI UX score | 5.5/10 | 7/10 | 9/10 | 10/10 |
| Extension UX score | 6/10 | 7.5/10 | 9/10 | 10/10 |
| Steps to first capture | 4 | 3 | 2 | 1 (`apix run`) |
| SDK languages supported | 0 | 0 | 0 | 3 |

---

## How to Contribute

1. Pick an issue from the current phase
2. Check the dependency graph — ensure prerequisites are done
3. Create a feature branch: `feat/<issue-number>-short-description`
4. Follow [CONTRIBUTING.md](../CONTRIBUTING.md) and [design principles](ARCHITECTURE/design-principles.md)
5. Submit PR referencing the issue: `Closes #NNN`

Issues labeled `S` effort are great first contributions.
