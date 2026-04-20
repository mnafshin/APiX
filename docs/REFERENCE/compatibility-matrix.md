# Compatibility Matrix

Use this matrix to match engine, CLI, extension, and API contract versions.

| Engine line | CLI line | VS Code extension line | gRPC API version (`GetVersion.api_version`) | Minimum client version (`GetVersion.min_client_version`) |
|---|---|---|---|---|
| 2.1.x | 2.1.x | 2.1.x | 1.0.0 | 1.0.0 |
| 2.0.x | 2.0.x | 2.0.x | 1.0.0 | 1.0.0 |

## Rules

1. Keep engine and CLI on the same minor line.
2. Extension should match the engine minor line.
3. Clients older than `min_client_version` are unsupported.

## How to check runtime versions

Use the CLI:

```bash
./apix-cli status
```

For API contract values, call `GetVersion` via gRPC tooling or client code.

