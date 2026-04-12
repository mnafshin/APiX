# Testing Strategy & Release Safety

Purpose

This document explains current and planned testing layers and how they map to project risks.

Layers
- Unit tests (Go/TS) — fast, exercise logic
- Integration tests — engine + storage + proxy behavior
- E2E tests — engine + extension + CLI in an integration environment
- Resilience/fault-injection — simulate upstream failures and flaky networks
- Release smoke tests — minimal checks that the built binaries start and respond

Actions for contributors
- Add tests near the code you change
- Use existing `make test` and CI jobs

Acceptance criteria
- Document maps each test to a risk and points to existing tests in tests/ (contract, e2e, integration)
