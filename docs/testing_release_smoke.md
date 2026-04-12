# Release Smoke Matrix for Engine, Extension, and CLI

Problem

Packaging and wiring issues (missing assets, stale generated code, broken entry points) often only appear post-build. A lightweight release smoke matrix reduces the risk of shipping broken artifacts.

Proposal

Add a small release-smoke suite that starts built artifacts and exercises a minimal end-to-end flow: start engine binary, perform a tiny capture, validate CLI help and one live command, and check extension packaging basics.

Acceptance criteria

- Release job runs smoke checks against built artifacts
- Failures block release publication until resolved
- Tests remain fast and reliable

First tasks

1. Add docs/testing_release_smoke.md (this file).
2. Add make target smoke that builds and runs the minimal checks.
3. Integrate smoke checks into release workflow (follow-up PR).

Relevant code

- cmd/apix-engine — engine binary
- cmd/apix-cli — CLI
- apix-vscode/ — extension packaging

Notes

Keep smoke checks minimal (seconds) and focused on packaging/runtime correctness rather than full E2E behavior.