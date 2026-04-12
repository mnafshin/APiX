# MCP Contract & Transcript Regression Suite

Problem

MCP (machine collaboration protocol) will be a machine-facing contract surface. Small changes in response shapes, error formats, or tooling semantics can silently break automation or agent pipelines.

Proposal

Provide a focused test suite that validates MCP shapes, sample transcripts, and tool argument validation. Tests should be fast, machine-readable, and suitable for CI gating. Implement golden transcript tests that replay representative agent interactions and assert stable output shapes and error behaviors.

Acceptance criteria

- Representative MCP schemas and example transcripts live in tests/mcp/
- CI runs quick transcript regressions and fails on shape or semantic drift
- Tests include tool argument validation and error-handling cases

First tasks

1. Add docs/testing_mcp_suite.md (this file).
2. Create tests/mcp/sample_transcripts/ with 2-3 golden transcripts (follow-up PR).
3. Add a lightweight test harness that runs transcripts and compares normalized JSON shapes.

Relevant code

- pkg/api/proto/apix.proto — source of truth for engine messages
- tests/ — add tests/mcp/ for transcripts and harness

Notes

Keep transcript fixtures small and focus on contract stability rather than full functional coverage.