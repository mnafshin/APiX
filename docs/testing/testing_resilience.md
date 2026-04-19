# Fault-Injection & Resilience Test Suite

Problem

Long-lived streams, network unreliability, and upstream failures are realistic in user environments. Without explicit resilience tests, regressions in reconnect logic, retry paths, and stream handling will surface in production.

Proposal

Add fault-injection tests that simulate upstream timeouts, resets, malformed responses, and gRPC stream interruptions. Use controlled test doubles (httptest servers, proxy hooks) to inject failures and assert client/engine recovery behavior.

Acceptance criteria

- Fault-injection harness under tests/resilience/
- Tests for upstream resets/timeouts, gRPC disconnects, and engine restart recovery
- CI includes critical resilience tests in integration stage

First tasks

1. Add docs/testing_resilience.md (this file).
2. Implement test helpers that toggle simulated upstream failure modes.
3. Add example tests: slow body -> timeout, upstream reset during read, gRPC stream interruption and reconnect.

Relevant code

- internal/proxy — handleHTTP, TLS MITM flow
- internal/server/grpc.go — streaming clients and interceptors
- tests/integration — integrate resilience checks

Notes

Keep injected failures deterministic and parameterizable to avoid flaky tests.