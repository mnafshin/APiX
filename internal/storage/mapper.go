package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	logging "github.com/mnafshin/apix/internal/logging"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// scanRequest scans a single request row from a *sql.Row.
func scanRequest(row *sql.Row) (*RequestRecord, error) {
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
func scanResponse(row *sql.Row) (*ResponseRecord, error) {
	var (
		reqID, statusText, hdrs string
		statusCode              int
		body                    []byte
	)
	if err := row.Scan(&reqID, &statusCode, &statusText, &hdrs, &body); err != nil {
		return nil, err
	}
	// SQLite returns nil for empty BLOBs; normalise to a non-nil empty slice so
	// callers can distinguish "no response" from "response with empty body".
	if body == nil {
		body = []byte{}
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

// scanTransactionRows scans a joined requests+responses result set into slices.
// A nil entry is appended to resps when a row has no matching response.
func scanTransactionRows(rows *sql.Rows) ([]*RequestRecord, []*ResponseRecord, error) {
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
			// SQLite returns nil for empty BLOBs; normalise to a non-nil empty slice
			// so callers can distinguish "response with empty body" from "no response".
			if respBody == nil {
				respBody = []byte{}
			}
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
