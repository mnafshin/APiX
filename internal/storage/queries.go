package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"strings"
	"time"
)

// Mapping functions (scanRequest, scanResponse, scanTransactionRows,
// scanRewriteRule, scanRewriteRuleRow, buildRewriteRule) live in mapper.go.

// RequestRecord is the Go representation of a row in the requests table.
type RequestRecord struct {
	ID         string
	Method     string
	URL        string
	Headers    map[string]string
	Body       []byte
	Timestamp  time.Time
	DurationMs int64
	Protocol   string // negotiated protocol: "HTTP/1.1", "HTTP/2.0", "h2c"
}

// ResponseRecord is the Go representation of a row in the responses table.
type ResponseRecord struct {
	RequestID  string
	StatusCode int
	StatusText string
	Headers    map[string]string
	Body       []byte
}

// BreakpointRecord is the Go representation of a row in the breakpoints table.
type BreakpointRecord struct {
	ID          string
	URLPattern  string
	Methods     []string
	Enabled     bool
	Label       string
	HeaderName  string
	HeaderValue string
	BodyPattern string
	StatusCodes []int32
	CreatedAt   time.Time
}

// RequestTemplateRecord is the Go representation of a row in request_templates.
type RequestTemplateRecord struct {
	ID        string
	Name      string
	Method    string
	URL       string
	Headers   map[string]string
	Body      []byte
	UpdatedAt time.Time
}

// SaveRequest inserts a request record (upsert on conflict).
func (d *DB) SaveRequest(r *RequestRecord) error {
	hdrs, err := json.Marshal(r.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO requests (id, method, url, headers, body, timestamp, duration_ms, protocol)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Method, r.URL, string(hdrs), r.Body,
		r.Timestamp.UnixMilli(), r.DurationMs, r.Protocol,
	)
	return err
}

// SaveResponse inserts or replaces a response record.
func (d *DB) SaveResponse(r *ResponseRecord) error {
	hdrs, err := json.Marshal(r.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO responses (request_id, status_code, status_text, headers, body)
		 VALUES (?, ?, ?, ?, ?)`,
		r.RequestID, r.StatusCode, r.StatusText, string(hdrs), r.Body,
	)
	return err
}

// GetTransaction retrieves a request+response pair by request ID.
func (d *DB) GetTransaction(id string) (*RequestRecord, *ResponseRecord, error) {
	req, err := scanRequest(d.db.QueryRow(
		`SELECT id, method, url, headers, body, timestamp, duration_ms, COALESCE(protocol,'HTTP/1.1')
		 FROM requests WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get request: %w", err)
	}

	resp, err := scanResponse(d.db.QueryRow(
		`SELECT request_id, status_code, status_text, headers, body
		 FROM responses WHERE request_id = ?`, id))
	if err == sql.ErrNoRows {
		return req, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get response: %w", err)
	}
	return req, resp, nil
}

// ListTransactions returns paginated request/response pairs matching the query.
// bodyFilter, when non-empty, restricts results to transactions where the
// request body OR response body contains the given substring.
func (d *DB) ListTransactions(limit, offset int, urlFilter, methodFilter string, statusFilter int, bodyFilter string) ([]*RequestRecord, []*ResponseRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	var whereClauses []string
	var args []interface{}

	if urlFilter != "" {
		whereClauses = append(whereClauses, "r.url LIKE ?")
		args = append(args, "%"+urlFilter+"%")
	}
	if methodFilter != "" {
		whereClauses = append(whereClauses, "r.method = ?")
		args = append(args, strings.ToUpper(methodFilter))
	}
	if statusFilter > 0 {
		whereClauses = append(whereClauses, "resp.status_code = ?")
		args = append(args, statusFilter)
	}
	if bodyFilter != "" {
		whereClauses = append(whereClauses, "(CAST(r.body AS TEXT) LIKE ? OR CAST(resp.body AS TEXT) LIKE ?)")
		pattern := "%" + bodyFilter + "%"
		args = append(args, pattern, pattern)
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT r.id, r.method, r.url, r.headers, r.body, r.timestamp, r.duration_ms,
		       COALESCE(r.protocol,'HTTP/1.1'),
		       resp.request_id, resp.status_code, resp.status_text, resp.headers, resp.body
		FROM requests r
		LEFT JOIN responses resp ON r.id = resp.request_id
		%s
		ORDER BY r.timestamp DESC
		LIMIT ? OFFSET ?`, where)

	args = append(args, limit, offset)
	return d.listTransactionsQuery(query, args...)
}

// ExportTransactions returns stored transactions for HAR export.
// When transactionIDs is empty, all stored transactions are returned.
func (d *DB) ExportTransactions(transactionIDs []string) ([]*RequestRecord, []*ResponseRecord, error) {
	query := `
		SELECT r.id, r.method, r.url, r.headers, r.body, r.timestamp, r.duration_ms,
		       COALESCE(r.protocol,'HTTP/1.1'),
		       resp.request_id, resp.status_code, resp.status_text, resp.headers, resp.body
		FROM requests r
		LEFT JOIN responses resp ON r.id = resp.request_id`
	args := make([]interface{}, 0, len(transactionIDs))
	if len(transactionIDs) > 0 {
		placeholders := make([]string, len(transactionIDs))
		for i, id := range transactionIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += " WHERE r.id IN (" + strings.Join(placeholders, ", ") + ")"
	}
	query += " ORDER BY r.timestamp ASC"
	return d.listTransactionsQuery(query, args...)
}

// DeleteAllTransactions removes all rows from requests (cascades to responses).
func (d *DB) DeleteAllTransactions() error {
	_, err := d.db.Exec("DELETE FROM requests")
	return err
}

// SaveBreakpoint inserts or replaces a breakpoint record.
func (d *DB) SaveBreakpoint(id, urlPattern string, methods []string, enabled bool, label, headerName, headerValue, bodyPattern string, statusCodes []int32) error {
	methodsJSON, err := json.Marshal(methods)
	if err != nil {
		return fmt.Errorf("marshal methods: %w", err)
	}
	statusCodesJSON, err := json.Marshal(statusCodes)
	if err != nil {
		return fmt.Errorf("marshal status codes: %w", err)
	}
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO breakpoints (id, url_pattern, methods, enabled, label, header_name, header_value, body_pattern, status_codes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, urlPattern, string(methodsJSON), enabledInt, label, headerName, headerValue, bodyPattern, string(statusCodesJSON), time.Now().UnixMilli(),
	)
	return err
}

// DeleteBreakpoint removes a breakpoint by ID.
func (d *DB) DeleteBreakpoint(id string) error {
	_, err := d.db.Exec("DELETE FROM breakpoints WHERE id = ?", id)
	return err
}

// ListBreakpoints returns all breakpoint records.
func (d *DB) ListBreakpoints() ([]*BreakpointRecord, error) {
	rows, err := d.db.Query(
		`SELECT id, url_pattern, methods, enabled, label, header_name, header_value, body_pattern, status_codes, created_at FROM breakpoints`,
	)
	if err != nil {
		return nil, fmt.Errorf("list breakpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bps []*BreakpointRecord
	for rows.Next() {
		var (
			id, urlPattern, methodsJSON, label, headerName, headerValue, bodyPattern, statusCodesJSON string
			enabledInt                                                                                int
			createdAtMs                                                                               int64
		)
		if err := rows.Scan(&id, &urlPattern, &methodsJSON, &enabledInt, &label, &headerName, &headerValue, &bodyPattern, &statusCodesJSON, &createdAtMs); err != nil {
			return nil, fmt.Errorf("scan breakpoint: %w", err)
		}
		bp := &BreakpointRecord{
			ID:          id,
			URLPattern:  urlPattern,
			Enabled:     enabledInt == 1,
			Label:       label,
			HeaderName:  headerName,
			HeaderValue: headerValue,
			BodyPattern: bodyPattern,
			CreatedAt:   time.UnixMilli(createdAtMs),
		}
		if err := json.Unmarshal([]byte(methodsJSON), &bp.Methods); err != nil {
			logging.Warnf(context.Background(), "failed to unmarshal methods for breakpoint %s: %v", id, err)
			bp.Methods = nil
		}
		if err := json.Unmarshal([]byte(statusCodesJSON), &bp.StatusCodes); err != nil {
			logging.Warnf(context.Background(), "failed to unmarshal status codes for breakpoint %s: %v", id, err)
			bp.StatusCodes = nil
		}
		bps = append(bps, bp)
	}
	return bps, rows.Err()
}

// SaveRequestTemplate inserts or replaces a request template by ID.
func (d *DB) SaveRequestTemplate(tpl *RequestTemplateRecord) error {
	hdrs, err := json.Marshal(tpl.Headers)
	if err != nil {
		return fmt.Errorf("marshal template headers: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO request_templates (id, name, method, url, headers, body, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tpl.ID, tpl.Name, tpl.Method, tpl.URL, string(hdrs), tpl.Body, tpl.UpdatedAt.UnixMilli(),
	)
	return err
}

// ListRequestTemplates returns templates ordered by most-recent update first.
func (d *DB) ListRequestTemplates() ([]*RequestTemplateRecord, error) {
	rows, err := d.db.Query(
		`SELECT id, name, method, url, headers, body, updated_at
		 FROM request_templates
		 ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list request templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var templates []*RequestTemplateRecord
	for rows.Next() {
		var (
			id, name, method, rawURL, hdrs string
			body                           []byte
			updatedAtMs                    int64
		)
		if err := rows.Scan(&id, &name, &method, &rawURL, &hdrs, &body, &updatedAtMs); err != nil {
			return nil, fmt.Errorf("scan request template: %w", err)
		}
		tpl := &RequestTemplateRecord{
			ID:        id,
			Name:      name,
			Method:    method,
			URL:       rawURL,
			Body:      body,
			UpdatedAt: time.UnixMilli(updatedAtMs),
		}
		if err := json.Unmarshal([]byte(hdrs), &tpl.Headers); err != nil {
			logging.Warnf(context.Background(), "failed to unmarshal template headers for id %s: %v", id, err)
			tpl.Headers = map[string]string{}
		}
		templates = append(templates, tpl)
	}
	return templates, rows.Err()
}

// DeleteRequestTemplate removes a request template by ID.
func (d *DB) DeleteRequestTemplate(id string) error {
	_, err := d.db.Exec(`DELETE FROM request_templates WHERE id = ?`, id)
	return err
}

func (d *DB) listTransactionsQuery(query string, args ...interface{}) ([]*RequestRecord, []*ResponseRecord, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list transactions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTransactionRows(rows)
}

// AddRewriteRule inserts a new rewrite rule.
func (d *DB) AddRewriteRule(rule *apix.RewriteRule) error {
	var match *apix.MatchCriteria
	if rule.Match != nil {
		match = rule.Match
	} else {
		match = &apix.MatchCriteria{}
	}
	enabledInt := 0
	if rule.Enabled {
		enabledInt = 1
	}
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO rewrite_rules
 (id, name, enabled, priority, url_pattern, method, header_name, header_value,
  body_pattern, status_code, action, param_key, param_value, body_template,
  response_status, response_body, response_content_type)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.Id, rule.Name, enabledInt, rule.Priority,
		match.UrlPattern, match.Method, match.HeaderName, match.HeaderValue,
		match.BodyPattern, match.StatusCode,
		int32(rule.Action), rule.ParamKey, rule.ParamValue, rule.BodyTemplate,
		rule.ResponseStatus, rule.ResponseBody, rule.ResponseContentType,
	)
	return err
}

// UpdateRewriteRule replaces an existing rewrite rule.
func (d *DB) UpdateRewriteRule(rule *apix.RewriteRule) error {
	return d.AddRewriteRule(rule)
}

// DeleteRewriteRule removes a rewrite rule by ID.
func (d *DB) DeleteRewriteRule(id string) error {
	_, err := d.db.Exec("DELETE FROM rewrite_rules WHERE id = ?", id)
	return err
}

// GetRewriteRule retrieves a single rewrite rule by ID.
func (d *DB) GetRewriteRule(id string) (*apix.RewriteRule, error) {
	row := d.db.QueryRow(
		`SELECT id, name, enabled, priority, url_pattern, method, header_name, header_value,
        body_pattern, status_code, action, param_key, param_value, body_template,
        response_status, response_body, response_content_type
 FROM rewrite_rules WHERE id = ?`, id)
	rule, err := scanRewriteRule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rule, err
}

// ListRewriteRules returns all rewrite rules ordered by priority.
func (d *DB) ListRewriteRules() ([]*apix.RewriteRule, error) {
	rows, err := d.db.Query(
		`SELECT id, name, enabled, priority, url_pattern, method, header_name, header_value,
        body_pattern, status_code, action, param_key, param_value, body_template,
        response_status, response_body, response_content_type
 FROM rewrite_rules ORDER BY priority ASC`)
	if err != nil {
		return nil, fmt.Errorf("list rewrite rules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var rules []*apix.RewriteRule
	for rows.Next() {
		rule, err := scanRewriteRuleRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan rewrite rule: %w", err)
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}
