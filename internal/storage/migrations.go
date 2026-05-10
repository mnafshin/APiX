package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

const storageSchemaVersion = 1

type storageMigration struct {
	version int
	name    string
	up      func(tx *sql.Tx) error
}

var storageMigrations = []storageMigration{
	{
		version: 1,
		name:    "breakpoints-match-extensions",
		up: func(tx *sql.Tx) error {
			ddls := []string{
				`ALTER TABLE breakpoints ADD COLUMN header_name TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE breakpoints ADD COLUMN header_value TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE breakpoints ADD COLUMN body_pattern TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE breakpoints ADD COLUMN status_codes TEXT NOT NULL DEFAULT '[]'`,
			}
			for _, ddl := range ddls {
				if err := execMigrationDDL(tx, ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
}

func applyStorageMigrations(db *sql.DB) error {
	if err := validateStorageMigrations(storageMigrations); err != nil {
		return err
	}

	currentVersion, err := readStorageUserVersion(db)
	if err != nil {
		return err
	}
	if currentVersion > storageSchemaVersion {
		return fmt.Errorf(
			"database schema version %d is newer than this binary supports (%d); upgrade APiX",
			currentVersion, storageSchemaVersion,
		)
	}

	for _, migration := range storageMigrations {
		if migration.version <= currentVersion {
			continue
		}
		if err := runStorageMigration(db, migration); err != nil {
			return err
		}
		currentVersion = migration.version
	}
	return nil
}

func validateStorageMigrations(migrations []storageMigration) error {
	last := 0
	for _, migration := range migrations {
		if migration.version <= last {
			return fmt.Errorf("storage migrations must be strictly increasing: got %d after %d", migration.version, last)
		}
		if migration.version != last+1 {
			return fmt.Errorf("storage migrations must be contiguous: missing version %d", last+1)
		}
		if migration.up == nil {
			return fmt.Errorf("storage migration v%d (%s) has no up function", migration.version, migration.name)
		}
		last = migration.version
	}
	if last != storageSchemaVersion {
		return fmt.Errorf("storage schema version mismatch: latest migration v%d != storageSchemaVersion v%d", last, storageSchemaVersion)
	}
	return nil
}

func runStorageMigration(db *sql.DB, migration storageMigration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin storage migration v%d (%s): %w", migration.version, migration.name, err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := migration.up(tx); err != nil {
		return fmt.Errorf("apply storage migration v%d (%s): %w", migration.version, migration.name, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", migration.version)); err != nil {
		return fmt.Errorf("set storage schema version v%d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit storage migration v%d (%s): %w", migration.version, migration.name, err)
	}
	return nil
}

func readStorageUserVersion(db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read storage schema version: %w", err)
	}
	return version, nil
}

func execMigrationDDL(tx *sql.Tx, ddl string) error {
	if _, err := tx.Exec(ddl); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}
