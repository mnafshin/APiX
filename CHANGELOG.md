# Changelog

<!-- markdownlint-disable MD024 -->

All notable changes to APiX are documented in this file.

The format is inspired by [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Docs contract snapshots for CLI help, config keys, and proto RPC surface.
- CI enforcement that user-facing docs contracts stay in sync with code changes.

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
