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
    duration_ms INTEGER NOT NULL DEFAULT 0,
    protocol    TEXT NOT NULL DEFAULT 'HTTP/1.1',  -- negotiated protocol
    body_size   INTEGER NOT NULL DEFAULT 0
);`

const CreateResponsesTable = `
CREATE TABLE IF NOT EXISTS responses (
    request_id  TEXT PRIMARY KEY,
    status_code INTEGER NOT NULL,
    status_text TEXT NOT NULL DEFAULT '',
    headers     TEXT NOT NULL DEFAULT '{}', -- JSON object
    body        BLOB,
    body_size   INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (request_id) REFERENCES requests(id) ON DELETE CASCADE
);`

const CreateBreakpointsTable = `
CREATE TABLE IF NOT EXISTS breakpoints (
    id          TEXT PRIMARY KEY,           -- UUID v4
    url_pattern TEXT NOT NULL,
    methods     TEXT NOT NULL DEFAULT '[]', -- JSON array of HTTP methods
    enabled     INTEGER NOT NULL DEFAULT 1, -- 0 = disabled, 1 = enabled
    label       TEXT NOT NULL DEFAULT '',
    header_name TEXT NOT NULL DEFAULT '',
    header_value TEXT NOT NULL DEFAULT '',
    body_pattern TEXT NOT NULL DEFAULT '',
    status_codes TEXT NOT NULL DEFAULT '[]', -- JSON array of response status codes
    created_at  INTEGER NOT NULL            -- Unix milliseconds
);`

const CreatePluginsTable = `
CREATE TABLE IF NOT EXISTS plugins (
    name       TEXT PRIMARY KEY,
    version    TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,  -- 0 = disabled, 1 = enabled
    config     TEXT NOT NULL DEFAULT '{}'   -- JSON object of plugin config
);`

const CreateWebSocketFramesTable = `
CREATE TABLE IF NOT EXISTS ws_frames (
    id             TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL,
    direction      TEXT NOT NULL,
    opcode         INTEGER NOT NULL,
    payload        BLOB,
    timestamp      INTEGER NOT NULL,
    FOREIGN KEY (transaction_id) REFERENCES requests(id) ON DELETE CASCADE
);`

const CreateRewriteRulesTable = `
CREATE TABLE IF NOT EXISTS rewrite_rules (
    id                    TEXT PRIMARY KEY,
    name                  TEXT NOT NULL DEFAULT '',
    enabled               INTEGER NOT NULL DEFAULT 1,
    priority              INTEGER NOT NULL DEFAULT 100,
    url_pattern           TEXT NOT NULL DEFAULT '',
    method                TEXT NOT NULL DEFAULT '',
    header_name           TEXT NOT NULL DEFAULT '',
    header_value          TEXT NOT NULL DEFAULT '',
    body_pattern          TEXT NOT NULL DEFAULT '',
    status_code           INTEGER NOT NULL DEFAULT 0,
    action                INTEGER NOT NULL DEFAULT 0,
    param_key             TEXT NOT NULL DEFAULT '',
    param_value           TEXT NOT NULL DEFAULT '',
    body_template         BLOB,
    response_status       INTEGER NOT NULL DEFAULT 200,
    response_body         BLOB,
    response_content_type TEXT NOT NULL DEFAULT ''
);`

const CreateRequestTemplatesTable = `
CREATE TABLE IF NOT EXISTS request_templates (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL,
    url         TEXT NOT NULL,
    headers     TEXT NOT NULL DEFAULT '{}',
    body        BLOB,
    updated_at  INTEGER NOT NULL
);`

// AllTables is the ordered list of DDL statements to apply during DB init.
// Order matters for foreign key dependencies.
var AllTables = []string{
	CreateRequestsTable,
	CreateResponsesTable,
	CreateBreakpointsTable,
	CreatePluginsTable,
	CreateWebSocketFramesTable,
	CreateRewriteRulesTable,
	CreateRequestTemplatesTable,
	// Indexes — created after tables so IF NOT EXISTS is safe on re-open.
	`CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_requests_method    ON requests(method)`,
	`CREATE INDEX IF NOT EXISTS idx_requests_url       ON requests(url)`,
	`CREATE INDEX IF NOT EXISTS idx_requests_method_url_ts ON requests(method, url, timestamp DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_responses_status   ON responses(status_code)`,
	`CREATE INDEX IF NOT EXISTS idx_ws_frames_txid     ON ws_frames(transaction_id)`,
	`CREATE INDEX IF NOT EXISTS idx_rewrite_rules_priority ON rewrite_rules(priority ASC)`,
	`CREATE INDEX IF NOT EXISTS idx_request_templates_updated_at ON request_templates(updated_at DESC)`,
}
