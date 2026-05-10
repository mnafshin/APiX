# Changelog

<!-- markdownlint-disable MD024 -->

All notable changes to APiX are documented in this file.

The format is inspired by [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Docs contract snapshots for CLI help, config keys, and proto RPC surface.
- CI enforcement that user-facing docs contracts stay in sync with code changes.

## [v2.3.0] - 2026-05-11

### Added
- **APiX Contract Model** – Define reusable contracts as source-of-truth for API lifecycle with JSON Schema validation
- **OpenAPI 3.1 Bidirectional Mapping** – Export contracts to OpenAPI YAML/JSON and import OpenAPI specs as contracts
- **AI Inference (`apix learn`)** – Generate draft contract/OpenAPI proposals from captured traffic with intelligent schema inference
- **Plugin Execution Security** – Configurable execution timeout (default 10s) and memory tracking to prevent runaway plugins
- **mTLS Authentication for gRPC** – Require and verify client certificates for gRPC API access (Phase 1)
- **Database Migration Versioning** – Enforce schema versioning with forward/backward compatibility tracking

### CLI
- `apix contract validate [file]` – Validate contract against APiX spec
- `apix contract export-openapi [contract] --output [openapi.yaml]` – Export contract to OpenAPI 3.1
- `apix contract import-openapi [openapi.yaml] --output [contract.json]` – Import OpenAPI as contract
- `apix learn [--from-traffic]` – Auto-generate contract proposals from observed traffic

### Security
- Plugin timeouts + memory limits prevent engine destabilization
- mTLS client authentication for all gRPC API calls
- Strict config permission checks (0600 for sensitive files)

### Configuration
- `plugin_execution_timeout_sec` (default: 10) – Max plugin execution time
- `grpc_client_ca_path` – Path to CA certificate for mTLS client auth
- `grpc_client_auth` (bool, default: false) – Enable mTLS client authentication

## [v2.1.0] - 2026-04-20

### Added
- Proxy request hardening for URL/header bounds and gRPC header-list size limits.
- Proxy-layer rate limiting and concurrent-connection controls.
- Structured audit logging for state-changing gRPC operations.
- Secure auth token loading with `auth_token_file` and strict config permission checks.
- VS Code extension token handling via `SecretStorage`.

### Changed
- Expanded architecture and deployment documentation.
- Improved traffic refresh behavior in the VS Code extension.

## [v2.0.0] - 2026-04-18

### Added
- Foundation release with HTTP/HTTPS interception, breakpoints, replay, storage, CLI, and VS Code integration.
