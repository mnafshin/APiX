# Stateful Workflow Tests for Breakpoints, Paused Requests, Rules, and Replay

Problem

APiX exposes stateful flows (breakpoints, paused requests, replay) where correctness depends on transitions across components. Unit tests alone are insufficient to catch lifecycle regressions and race conditions.

Proposal

Introduce stateful tests encoding lifecycle scenarios (breakpoint add -> hit -> pause -> resume/drop/respond -> cleanup). Implement reusable scenario helpers that drive the engine, inject simulated clients, and assert end states. Favor deterministic scenarios and isolate timing-sensitive steps with explicit synchronization.

Acceptance criteria

- Reusable scenario harness in tests/stateful/
- Core scenarios implemented as fast unit/integration tests
- Clear assertions for invalid transitions and cleanup behavior

First tasks

1. Create tests/stateful/ scenario harness (follow-up PR).
2. Encode one canonical flow: breakpoint hit → pause → respond → resume.
3. Add CI job that runs critical stateful scenarios in pre-merge checks.

Relevant code

- internal/breakpoints — matching & pause orchestration
- internal/engine — StoreTransaction, PauseRequest
- apix-vscode extension for reference client flows (apix-vscode/src/requestEditor.ts)

Notes

Start with deterministic synchronous scenarios and expand to concurrency tests after harness is stable.