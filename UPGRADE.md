# Upgrade Guide

This guide covers safe upgrades for APiX engine, CLI, and VS Code extension.

## Supported upgrade path

- **Recommended:** upgrade within the same minor line first (for example `2.0.x -> 2.1.x`).
- Upgrade engine + CLI together for predictable behavior.
- Update the VS Code extension after the engine upgrade.

Check version compatibility in [`docs/REFERENCE/compatibility-matrix.md`](docs/REFERENCE/compatibility-matrix.md).

## Pre-upgrade checklist

1. Back up your config (`~/.apix/config.yaml` or deployment-managed equivalent).
2. Back up your storage DB if you keep long-lived history.
3. Confirm whether you use plaintext `auth_token`; prefer `APIX_AUTH_TOKEN` or `auth_token_file`.
4. Read the release section in [`CHANGELOG.md`](CHANGELOG.md) for your target version.

## Upgrade steps

1. Install new engine and CLI binaries.
2. Update the VS Code extension to the matching release line.
3. Run config validation:
   ```bash
   ./apix-engine --config-check
   ```
4. Start the engine and verify:
   ```bash
   ./apix-engine
   ./apix-cli status
   ./apix-cli doctor
   ```

## v2.0.x -> v2.1.x notes

- If you currently store `auth_token` in config, APiX now enforces strict file permissions by default.
- You can migrate secrets to `auth_token_file` for cleaner secret management in deployments.
- New proxy safety knobs are available:
  - `proxy_rate_limit_per_sec`
  - `proxy_max_concurrent_connections`
  - request/header bound controls

## Rollback

1. Stop the upgraded engine.
2. Reinstall previous engine/CLI binaries.
3. Restore previous config if it was changed.
4. Start old version and run `./apix-cli status`.

If rollback is required because of compatibility, keep your previous version pinned and open an issue with the failing workflow and versions.

