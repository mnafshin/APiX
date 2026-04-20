# Versioning and Support Policy

This policy defines semantic versioning, support windows, and deprecation behavior across APiX public surfaces.

## Semantic versioning rules

APiX uses `MAJOR.MINOR.PATCH`.

| Surface | PATCH | MINOR | MAJOR |
|---|---|---|---|
| Engine / CLI binaries | bug fixes, non-breaking behavior fixes | additive commands/flags/options | breaking CLI behavior or workflow contract changes |
| gRPC API (`pkg/api/proto/apix.proto`) | non-breaking server fixes | additive fields/RPCs | breaking field/RPC changes |
| Config keys | bug fixes to existing key behavior | additive keys | removal or incompatible key behavior changes |
| VS Code extension | bug fixes | additive UX/features | breaking workflow/contract changes |
| Storage schema | non-breaking internal migrations | additive schema that preserves reads/writes | incompatible schema requiring manual migration |

## Support window

| Release line | Support level |
|---|---|
| Current minor (N) | full fixes (security, bugs, regressions) |
| Previous minor (N-1) | security + critical fixes |
| Older than N-1 | unsupported |

Supported versions are also listed in [`SECURITY.md`](../../SECURITY.md).

## Deprecation policy

1. Mark feature/key/command as deprecated in docs and release notes.
2. Keep deprecated behavior for at least one minor release when practical.
3. Provide migration steps in [`UPGRADE.md`](../../UPGRADE.md).
4. Remove only in a planned release with explicit notice.

## Release note labeling

Every release should label user-facing changes as:
- **Additive**
- **Breaking**
- **Deprecated**

These labels are reflected in `CHANGELOG.md` and generated release notes.
