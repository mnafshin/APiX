# APiX Documentation Index

Welcome to APiX documentation. Start here to find what you're looking for.

## Quick Navigation

**For end users & operators:**
- [`DEPLOYMENT.md`](DEPLOYMENT.md) — running APiX in production (Docker, systemd, Kubernetes)
- [`CONFIG_VALIDATION.md`](CONFIG_VALIDATION.md) — configuration schema and validation
- [`getting-started.md`](getting-started.md) — first steps with APiX
- [`FEATURES.md`](FEATURES.md) — complete feature reference (v2.0.0)

**For developers & contributors:**
- [`CONTRIBUTING.md`](../CONTRIBUTING.md) — how to contribute code and issues
- [Architecture](#architecture-concepts) — system design deep dives
- [Testing](#testing-strategy) — testing layers and strategies
- [Reference](#reference) — CLI, gRPC, config, glossary

**For AI integration:**
- [`REFERENCE/cli_mcp.md`](REFERENCE/cli_mcp.md) — MCP server setup and usage

---

## Getting Started

### 1. **First Time?**
Read [`getting-started.md`](getting-started.md) for a task-oriented walkthrough.

### 2. **Deploying to Production?**
See [`DEPLOYMENT.md`](DEPLOYMENT.md) for systemd, Docker, Kubernetes, and TLS setup.

### 3. **Need Help Configuring?**
Check [`CONFIG_VALIDATION.md`](CONFIG_VALIDATION.md) for all configuration keys and validation rules.

### 4. **Running How-to Workflows**
Browse [`how-to/`](how-to/) for step-by-step recipes:
- [First Breakpoint](how-to/first-breakpoint.md)
- [First Capture](how-to/first-capture.md)
- [Replay Requests](how-to/replay.md)
- [Plugin Development](how-to/plugin-development.md)
- [Plugin Configuration](how-to/plugin-configuration.md)

---

## Architecture & Concepts

Deep-dive technical documentation:

- [`ARCHITECTURE/design-principles.md`](ARCHITECTURE/design-principles.md) — core design tenets
- [`ARCHITECTURE/proxy_mitm.md`](ARCHITECTURE/proxy_mitm.md) — HTTP proxy and TLS MITM
- [`ARCHITECTURE/grpc_protobuf.md`](ARCHITECTURE/grpc_protobuf.md) — gRPC API contract
- [`ARCHITECTURE/storage-replay.md`](ARCHITECTURE/storage_replay.md) — SQLite storage and replay engine
- [`ARCHITECTURE/extension-arch.md`](ARCHITECTURE/extension_arch.md) — VS Code extension internals

---

## Reference

Stable contracts and schemas:

- [`REFERENCE/cli-contract-v1.md`](REFERENCE/cli-contract-v1.md) — CLI flags, output formats, exit codes
- [`REFERENCE/cli_mcp.md`](REFERENCE/cli_mcp.md) — MCP server integration and tools
- [`REFERENCE/glossary.md`](REFERENCE/glossary.md) — terminology and concepts
- [`REFERENCE/OTEL.md`](REFERENCE/OTEL.md) — OpenTelemetry and Prometheus metrics
- [`REFERENCE/plugin-sdk.md`](REFERENCE/plugin-sdk.md) — plugin interfaces and lifecycle

---

## Testing Strategy

Comprehensive testing coverage:

- [`TESTING.md`](TESTING.md) — overview of testing layers
- [`testing/testing_strategy.md`](testing/testing_strategy.md) — testing approach and best practices
- [`testing/testing_release_smoke.md`](testing/testing_release_smoke.md) — smoke tests
- [`testing/testing_resilience.md`](testing/testing_resilience.md) — fault injection and resilience testing
- [`testing/testing_stateful_workflows.md`](testing/testing_stateful_workflows.md) — end-to-end stateful workflows
- [`testing/testing_mcp_suite.md`](testing/testing_mcp_suite.md) — MCP integration tests

---

## Archive

Historical documents and outdated proposals:

- [`ARCHIVE/critique-audit.md`](ARCHIVE/critique-audit.md) — comprehensive code audit and critique
- [`ARCHIVE/review-summary.md`](ARCHIVE/review-summary.md) — external product review summary
- [`ARCHIVE/critique-initial-audit.md`](ARCHIVE/critique-initial-audit.md) — initial audit findings
- [`ARCHIVE/review-external.md`](ARCHIVE/review-external.md) — detailed external review
- [`ARCHIVE/initial-docs-structure-plan.md`](ARCHIVE/initial-docs-structure-plan.md) — earlier documentation IA proposal
- [`ARCHIVE/`](ARCHIVE/) — other historical documents

---

## Contributing to Docs

- Keep docs focused on a single purpose (tutorial, how-to, concept, or reference)
- Cross-link liberally; each page should point to what to read next
- Keep quickstarts short and actionable; reserve concepts for explanation
- Update this INDEX when adding new documentation

For code contributions, see [`CONTRIBUTING.md`](../CONTRIBUTING.md).
