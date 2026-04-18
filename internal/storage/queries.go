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
	ID         string
	URLPattern string
	Methods    []string
	Enabled    bool
	Label      string
	CreatedAt  time.Time
}

// SaveRequest inserts a request record (upsert on conflict).
func (d *DB) SaveRequest(r *RequestRecord) error {
	hdrs, err := json.Marshal(r.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO requests (id, method, url, headers, body, timestamp, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Method, r.URL, string(hdrs), r.Body,
		r.Timestamp.UnixMilli(), r.DurationMs,
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
	req, err := d.scanRequest(d.db.QueryRow(
		`SELECT id, method, url, headers, body, timestamp, duration_ms, COALESCE(protocol,'HTTP/1.1')
		 FROM requests WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get request: %w", err)
	}

	resp, err := d.scanResponse(d.db.QueryRow(
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
func (d *DB) SaveBreakpoint(id, urlPattern string, methods []string, enabled bool, label string) error {
	methodsJSON, err := json.Marshal(methods)
	if err != nil {
		return fmt.Errorf("marshal methods: %w", err)
	}
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err = d.db.Exec(
		`INSERT OR REPLACE INTO breakpoints (id, url_pattern, methods, enabled, label, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, urlPattern, string(methodsJSON), enabledInt, label, time.Now().UnixMilli(),
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
		`SELECT id, url_pattern, methods, enabled, label, created_at FROM breakpoints`,
	)
	if err != nil {
		return nil, fmt.Errorf("list breakpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bps []*BreakpointRecord
	for rows.Next() {
		var (
			id, urlPattern, methodsJSON, label string
			enabledInt                         int
			createdAtMs                        int64
		)
		if err := rows.Scan(&id, &urlPattern, &methodsJSON, &enabledInt, &label, &createdAtMs); err != nil {
			return nil, fmt.Errorf("scan breakpoint: %w", err)
		}
		bp := &BreakpointRecord{
			ID:         id,
			URLPattern: urlPattern,
			Enabled:    enabledInt == 1,
			Label:      label,
			CreatedAt:  time.UnixMilli(createdAtMs),
		}
		if err := json.Unmarshal([]byte(methodsJSON), &bp.Methods); err != nil {
			logging.Warnf(context.Background(), "failed to unmarshal methods for breakpoint %s: %v", id, err)
			bp.Methods = nil
		}
		bps = append(bps, bp)
	}
	return bps, rows.Err()
}

// scanRequest scans a single request row from a *sql.Row.
func (d *DB) scanRequest(row *sql.Row) (*RequestRecord, error) {
	var (
		id, method, url, hdrs string
		protocol              string
		body                  []byte
		tsMs, durMs           int64
	)
	if err := row.Scan(&id, &method, &url, &hdrs, &body, &tsMs, &durMs, &protocol); err != nil {
		return nil, err
	}
	req := &RequestRecord{
		ID:         id,
		Method:     method,
		URL:        url,
		Body:       body,
		Timestamp:  time.UnixMilli(tsMs),
		DurationMs: durMs,
		Protocol:   protocol,
	}
	if err := json.Unmarshal([]byte(hdrs), &req.Headers); err != nil {
		logging.Warnf(context.Background(), "failed to unmarshal request headers for request %s: %v", id, err)
		req.Headers = make(map[string]string)
	}
	return req, nil
}

// scanResponse scans a single response row from a *sql.Row.
func (d *DB) scanResponse(row *sql.Row) (*ResponseRecord, error) {
	var (
		reqID, statusText, hdrs string
		statusCode              int
		body                    []byte
	)
	if err := row.Scan(&reqID, &statusCode, &statusText, &hdrs, &body); err != nil {
		return nil, err
	}
	resp := &ResponseRecord{
		RequestID:  reqID,
		StatusCode: statusCode,
		StatusText: statusText,
		Body:       body,
	}
	if err := json.Unmarshal([]byte(hdrs), &resp.Headers); err != nil {
		logging.Warnf(context.Background(), "failed to unmarshal response headers for request %s: %v", reqID, err)
		resp.Headers = make(map[string]string)
	}
	return resp, nil
}

func (d *DB) listTransactionsQuery(query string, args ...interface{}) ([]*RequestRecord, []*ResponseRecord, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list transactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var reqs []*RequestRecord
	var resps []*ResponseRecord

	for rows.Next() {
		var (
			reqID, method, url, reqHeaders string
			protocol                       string
			reqBody                        []byte
			tsMs, durMs                    int64
			respReqID                      sql.NullString
			statusCode                     sql.NullInt64
			statusText, respHeaders        sql.NullString
			respBody                       []byte
		)
		if err := rows.Scan(
			&reqID, &method, &url, &reqHeaders, &reqBody, &tsMs, &durMs, &protocol,
			&respReqID, &statusCode, &statusText, &respHeaders, &respBody,
		); err != nil {
			return nil, nil, fmt.Errorf("scan transaction: %w", err)
		}

		req := &RequestRecord{
			ID:         reqID,
			Method:     method,
			URL:        url,
			Body:       reqBody,
			Timestamp:  time.UnixMilli(tsMs),
			DurationMs: durMs,
			Protocol:   protocol,
		}
		if err := json.Unmarshal([]byte(reqHeaders), &req.Headers); err != nil {
			logging.Warnf(context.Background(), "failed to unmarshal request headers for request %s: %v", reqID, err)
			req.Headers = make(map[string]string)
		}
		reqs = append(reqs, req)

		if respReqID.Valid {
			resp := &ResponseRecord{
				RequestID:  respReqID.String,
				StatusCode: int(statusCode.Int64),
				StatusText: statusText.String,
				Body:       respBody,
			}
			if respHeaders.Valid {
				if err := json.Unmarshal([]byte(respHeaders.String), &resp.Headers); err != nil {
					logging.Warnf(context.Background(), "failed to unmarshal response headers for request %s: %v", respReqID.String, err)
					resp.Headers = make(map[string]string)
				}
			}
			resps = append(resps, resp)
		} else {
			resps = append(resps, nil)
		}
	}
	return reqs, resps, rows.Err()
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

func scanRewriteRule(row *sql.Row) (*apix.RewriteRule, error) {
var (
id, name, urlPattern, method, headerName, headerValue  string
bodyPattern, paramKey, paramValue, responseContentType string
enabledInt, priority, statusCode, action, responseStatus int
bodyTemplate, responseBody                              []byte
)
err := row.Scan(
&id, &name, &enabledInt, &priority,
&urlPattern, &method, &headerName, &headerValue,
&bodyPattern, &statusCode, &action, &paramKey, &paramValue,
&bodyTemplate, &responseStatus, &responseBody, &responseContentType,
)
if err != nil {
return nil, err
}
return buildRewriteRule(id, name, enabledInt, priority, urlPattern, method, headerName, headerValue,
bodyPattern, statusCode, action, paramKey, paramValue, bodyTemplate, responseStatus, responseBody, responseContentType), nil
}

func scanRewriteRuleRow(rows *sql.Rows) (*apix.RewriteRule, error) {
var (
id, name, urlPattern, method, headerName, headerValue  string
bodyPattern, paramKey, paramValue, responseContentType string
enabledInt, priority, statusCode, action, responseStatus int
bodyTemplate, responseBody                              []byte
)
err := rows.Scan(
&id, &name, &enabledInt, &priority,
&urlPattern, &method, &headerName, &headerValue,
&bodyPattern, &statusCode, &action, &paramKey, &paramValue,
&bodyTemplate, &responseStatus, &responseBody, &responseContentType,
)
if err != nil {
return nil, err
}
return buildRewriteRule(id, name, enabledInt, priority, urlPattern, method, headerName, headerValue,
bodyPattern, statusCode, action, paramKey, paramValue, bodyTemplate, responseStatus, responseBody, responseContentType), nil
}

func buildRewriteRule(id, name string, enabledInt, priority int, urlPattern, method, headerName, headerValue,
	bodyPattern string, statusCode, action int, paramKey, paramValue string, bodyTemplate []byte,
	responseStatus int, responseBody []byte, responseContentType string) *apix.RewriteRule {
	return &apix.RewriteRule{
		Id:      id,
		Name:    name,
		Enabled: enabledInt == 1,
		Priority: int32(priority), //nolint:gosec // G115: priority is bounded by DB constraints
		Match: &apix.MatchCriteria{
			UrlPattern:  urlPattern,
			Method:      method,
			HeaderName:  headerName,
			HeaderValue: headerValue,
			BodyPattern: bodyPattern,
			StatusCode:  int32(statusCode), //nolint:gosec // G115: HTTP status codes fit in int32
		},
		Action:              apix.RewriteAction(action), //nolint:gosec // G115: action is a bounded enum from DB
		ParamKey:            paramKey,
		ParamValue:          paramValue,
		BodyTemplate:        bodyTemplate,
		ResponseStatus:      int32(responseStatus), //nolint:gosec // G115: HTTP status codes fit in int32
		ResponseBody:        responseBody,
		ResponseContentType: responseContentType,
	}
}
