# Storage Migration Policy

APiX uses SQLite `PRAGMA user_version` plus ordered code migrations to keep storage upgrades predictable.

## Versioning contract

1. Storage migrations are versioned as contiguous integers (`1, 2, 3, ...`).
2. Each migration is applied exactly once at startup when opening the DB.
3. Startup fails fast if a DB has a newer schema version than the running binary supports.

## Ordering and safety rules

1. Migrations must be strictly increasing and contiguous.
2. Each migration runs in a transaction.
3. `user_version` is updated only after migration steps succeed.
4. If a migration fails, startup fails and no partial version bump is committed.

## Compatibility expectations

- **Forward compatibility (new binary on old DB):** supported through migrations.
- **Backward compatibility (old binary on new DB):** not supported; APiX returns a clear startup error.

## Contributor workflow

When changing storage schema:

1. Add a new migration entry in `internal/storage/migrations.go`.
2. Bump `storageSchemaVersion`.
3. Add tests for upgrade and failure behavior in `internal/storage/storage_test.go`.
4. Update release notes/upgrade guidance when migration impacts users.
