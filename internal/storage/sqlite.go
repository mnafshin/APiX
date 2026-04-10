package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// DB wraps a *sql.DB with APiX-specific helpers.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	// Configure connection pool. For :memory: databases each new connection
	// creates a separate empty DB, so pin to exactly one connection.
	if path == ":memory:" {
		// In-memory DBs need a single connection to be shared.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		// Reasonable defaults for file-backed DB: keep some idle and allow multiple
		// concurrent connections for read-heavy workloads. These can be tuned later
		// or made configurable via a future config option.
		db.SetMaxOpenConns(25)     // max concurrent connections
		db.SetMaxIdleConns(5)      // keep 5 idle connections ready
	}

	// Lifetime and idle times help recycle connections and avoid long-lived
	// stale handles in long-running processes.
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	// Improve write performance and avoid excessive fsyncs while remaining
	// reasonably durable.
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}

	// Wait briefly for locks instead of failing immediately. This reduces
	// transient SQLITE_BUSY errors under moderate contention.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// Enforce foreign key constraints.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Apply all DDL statements from schema.go.
	for _, ddl := range AllTables {
		if _, err := db.Exec(ddl); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}

	return &DB{db: db}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}
