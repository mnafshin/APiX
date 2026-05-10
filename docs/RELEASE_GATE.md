# Release Gate Checklist

This checklist defines required validation before cutting a release tag.

## Validation matrix

| Surface | Required checks | Blocking |
|---|---|---|
| Engine + core Go packages | `go build ./...`, `go vet ./...`, `go test ./internal/... -race` | Yes |
| Performance regression budget | `make perf-check` | Yes |
| End-to-end behavior | `go test ./tests/e2e/...` | Yes |
| Proto contract | `buf lint`, `buf breaking` against `main` | Yes |
| VS Code extension | `npm ci`, `npx tsc --noEmit`, `npm run compile` | Yes |
| Docs contracts | `scripts/docs/verify_contract_snapshots.sh`, markdown lint/link checks | Yes |
| Release smoke | `make smoke` | Yes |
| Packaging artifacts | Engine + CLI binaries for release targets, VSIX package | Yes |
| Screenshots / UX visuals | Updated only when UI changed | No |

## Blocking gate

Release is blocked if any **blocking** check above fails.

## Pre-tag checklist

1. Milestone exit criteria met (see `docs/ROADMAP.md`).
2. `CHANGELOG.md` updated for release tag.
3. `UPGRADE.md` includes migration notes for behavior changes.
4. Compatibility and versioning docs are current.

## Post-release checklist

1. Publish GitHub release artifacts and notes.
2. Verify download links and extension package availability.
3. Announce release with upgrade guidance and known issues.
