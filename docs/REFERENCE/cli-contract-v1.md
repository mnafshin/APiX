# APiX CLI Contract v1

This document defines the **user-facing compatibility contract** for the first contract-first APiX CLI shipped from `cmd/apix-cli`, exposed as the `apix` command.

It is the reference for scripts, CI jobs, and future MCP/agent integrations that depend on stable CLI behavior. Implementation details may evolve internally, but the behavior described here should be treated as the compatibility surface for the v1 CLI.

`apix-cli` remains available as a compatibility alias; new docs and examples use `apix`.

## Contract scope

This contract covers:

- global connection, auth, and output flags
- command classes and output modes
- machine-readable success payloads
- machine-readable error payloads
- exit-code behavior
- retry and idempotency notes for write workflows

It does **not** guarantee:

- exact human-readable table/text formatting
- ordering of non-essential fields in JSON objects
- internal Go types or helper function names

## Supported output modes

APiX CLI supports three output modes:

| Mode | Intended use | Contract notes |
|---|---|---|
| `text` | humans in terminals | readable tables/messages; formatting may improve over time |
| `json` | unary/read-style automation | one JSON document per command result |
| `ndjson` | streaming automation | one JSON object per line for event streams |

### Command classes

| Command class | Output contract |
|---|---|
| unary read commands (`status`, `plugins list`, `history list|get`, `config show`, `cert status`) | `text` or `json` |
| streaming commands (`watch traffic`, `paused watch`) | `text` or `ndjson`; `json` behaves as line-delimited JSON events |
| write commands (`breakpoints add|delete|enable|disable`, `paused ...`, `send`, `replay`, `history clear`) | `text` or `json` |

## Global flags

The following global flags are part of the v1 contract:

| Flag | Meaning |
|---|---|
| `--host` | engine host |
| `--port` | engine gRPC port |
| `--tls` | use TLS for gRPC transport |
| `--token` | bearer token for authenticated engines |
| `--timeout` | per-command timeout |
| `--output text|json|ndjson` | output mode |
| `--config` | explicit config path |
| `--config-check` | validate config and exit |
| `--no-color` | reserved for text output behavior |

## Success payload rules

### General guarantees

- machine-readable field names are **snake_case**
- additive fields may be introduced in later versions, but existing documented fields should not be removed without a breaking-change notice
- streaming commands emit one JSON object per line

### Read commands

Representative stable fields:

| Command | Stable fields |
|---|---|
| `status --output json` | `status`, `version`, `proxy_port`, `grpc_port`, `tls_enabled` |
| `plugins list --output json` | array of objects with `name`, `version`, `description`, `enabled` |
| `history list --output json` | array of objects including `id`, `request_id`, `timestamp`, `duration_ms`, `request`, optional `response` |
| `history get --output json` | single object with the same field family as `history list` items; supports lookup by `id` or `--request-id` |
| `watch traffic --output ndjson` | one event object per line with `event`, `id`, `request_id`, `method`, `url`, `headers`, `body`, `timestamp` |

### Write commands

Representative stable fields:

| Command | Stable fields |
|---|---|
| `breakpoints add --output json` | `id`, `url_pattern`, `methods`, `enabled`, `label` |
| `breakpoints delete --output json` | `deleted` |
| `paused drop|forward|respond --output json` | `result`, `request_id` |
| `history clear --output json` | `cleared` |
| `send --output json` | `status_code`, `status_text`, `headers`, `body` |
| `replay --output json` | `status_code`, `status_text`, `headers`, `body` |

## Error contract

When a command is invoked with `--output json` or `--output ndjson`, APiX emits a machine-readable error envelope instead of prose-only stderr:

```json
{
  "error": {
    "code": "unauthenticated",
    "message": "rpc error: code = Unauthenticated desc = missing authorization metadata",
    "grpc_code": "Unauthenticated",
    "exit_code": 4
  }
}
```

### Error fields

| Field | Meaning |
|---|---|
| `code` | normalized machine-readable code such as `invalid_argument`, `not_found`, `unauthenticated`, `unavailable`, `deadline_exceeded`, `internal` |
| `message` | detailed error message |
| `grpc_code` | underlying gRPC code when available, otherwise `Unknown` |
| `exit_code` | CLI process exit code |

### Streaming errors

For streaming commands, runtime failures are emitted as a single JSON line on stderr using the same envelope shape. Scripts should treat stderr as the terminal error channel for a failed stream.

## Exit-code contract

| Exit code | Meaning |
|---|---|
| `0` | success |
| `2` | invalid argument / usage failure |
| `3` | not found |
| `4` | unauthenticated |
| `5` | permission denied |
| `6` | unavailable / connection failure |
| `7` | deadline exceeded |
| `1` | generic internal failure |

## Retry and idempotency notes

| Command family | Retry guidance |
|---|---|
| `status`, `plugins list`, `history list|get`, `config show`, `cert status` | safe to retry |
| `watch traffic`, `paused watch` | reconnect by starting a new stream; consumers should tolerate repeated events across reconnects |
| `breakpoints add` | not guaranteed idempotent; retry only with caller-side reconciliation |
| `breakpoints delete|enable|disable` | retryable if caller can tolerate already-applied state |
| `history clear` | destructive; only retry if caller accepts that history may already be cleared |
| `send`, `replay`, `paused forward|drop|respond` | may have side effects; do not blindly retry without workflow-level intent |

## Versioning note

Future CLI contract revisions should publish a new versioned document rather than silently changing this one. For example, a breaking machine-readable change should introduce a new contract page such as `docs/cli-contract-v2.md`.
