package storage

// Schema contains the DDL for all APiX SQLite tables.
// Each constant is a standalone CREATE TABLE IF NOT EXISTS statement so they
// can be executed individually or as the AllTables slice.

const CreateRequestsTable = `
CREATE TABLE IF NOT EXISTS requests (
    id          TEXT PRIMARY KEY,           -- UUID v4
    method      TEXT NOT NULL,
    url         TEXT NOT NULL,
    headers     TEXT NOT NULL DEFAULT '{}', -- JSON object
    body        BLOB,
    timestamp   INTEGER NOT NULL,           -- Unix milliseconds
    duration_ms INTEGER NOT NULL DEFAULT 0
);`

const CreateResponsesTable = `
CREATE TABLE IF NOT EXISTS responses (
    request_id  TEXT PRIMARY KEY,
    status_code INTEGER NOT NULL,
    status_text TEXT NOT NULL DEFAULT '',
    headers     TEXT NOT NULL DEFAULT '{}', -- JSON object
    body        BLOB,
    FOREIGN KEY (request_id) REFERENCES requests(id) ON DELETE CASCADE
);`

const CreateBreakpointsTable = `
CREATE TABLE IF NOT EXISTS breakpoints (
    id          TEXT PRIMARY KEY,           -- UUID v4
    url_pattern TEXT NOT NULL,
    methods     TEXT NOT NULL DEFAULT '[]', -- JSON array of HTTP methods
    enabled     INTEGER NOT NULL DEFAULT 1, -- 0 = disabled, 1 = enabled
    label       TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL            -- Unix milliseconds
);`

const CreatePluginsTable = `
CREATE TABLE IF NOT EXISTS plugins (
    name       TEXT PRIMARY KEY,
    version    TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,  -- 0 = disabled, 1 = enabled
    config     TEXT NOT NULL DEFAULT '{}'   -- JSON object of plugin config
);`

// AllTables is the ordered list of DDL statements to apply during DB init.
// Order matters for foreign key dependencies.
var AllTables = []string{
	CreateRequestsTable,
	CreateResponsesTable,
	CreateBreakpointsTable,
	CreatePluginsTable,
}
