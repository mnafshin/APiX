# Feature Stability Policy

APiX uses explicit stability labels for user-facing surfaces.

## Labels

| Label | Meaning | Upgrade expectation |
|---|---|---|
| `stable` | Supported behavior with compatibility intent across patch/minor upgrades. | No breaking changes without clear migration guidance. |
| `experimental` | Available for feedback; behavior may change quickly. | Breaking changes may happen between minor releases. |
| `deprecated` | Supported temporarily but scheduled for removal. | Migrate before the documented removal release. |

## Current defaults

| Surface | Stability |
|---|---|
| Engine and proxy core workflow | `stable` |
| gRPC API (`pkg/api/proto/apix.proto`, API v1.0.0) | `stable` |
| CLI contract v1 (`docs/REFERENCE/cli-contract-v1.md`) | `stable` |
| MCP side-effect tools (`mcp_allow_replay`, `mcp_allow_compose`) | `experimental` |

## Deprecation process

1. Add explicit deprecation note in docs and release notes.
2. Keep behavior available for at least one minor release when practical.
3. Provide migration steps in [`UPGRADE.md`](../../UPGRADE.md).

