package har

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/version"
)

// ImportedTransaction is a storage-ready request/response pair decoded from HAR.
type ImportedTransaction struct {
	Request  *storage.RequestRecord
	Response *storage.ResponseRecord
}

type archive struct {
	Log log `json:"log"`
}

type log struct {
	Version string     `json:"version"`
	Creator creator    `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Timings         harTimings  `json:"timings"`
}

type harTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

type harRequest struct {
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	HTTPVersion string   `json:"httpVersion"`
	Headers     []harKV  `json:"headers"`
	QueryString []harKV  `json:"queryString,omitempty"`
	HeadersSize int      `json:"headersSize"`
	BodySize    int      `json:"bodySize"`
	PostData    *harBody `json:"postData,omitempty"`
}

type harResponse struct {
	Status      int     `json:"status"`
	StatusText  string  `json:"statusText"`
	HTTPVersion string  `json:"httpVersion"`
	Headers     []harKV `json:"headers"`
	Content     harBody `json:"content"`
	RedirectURL string  `json:"redirectURL"`
	HeadersSize int     `json:"headersSize"`
	BodySize    int     `json:"bodySize"`
}

type harBody struct {
	Size     int    `json:"size,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type harKV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MarshalTransactions converts stored APiX traffic to HAR 1.2 JSON.
func MarshalTransactions(reqs []*storage.RequestRecord, resps []*storage.ResponseRecord) (string, error) {
	if len(reqs) != len(resps) {
		return "", fmt.Errorf("request/response count mismatch: %d vs %d", len(reqs), len(resps))
	}

	entries := make([]harEntry, 0, len(reqs))
	for i, req := range reqs {
		if req == nil {
			continue
		}
		entries = append(entries, exportEntry(req, resps[i]))
	}

	data, err := json.MarshalIndent(archive{
		Log: log{
			Version: "1.2",
			Creator: creator{Name: "APiX", Version: version.Version},
			Entries: entries,
		},
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal HAR: %w", err)
	}
	return string(data), nil
}

// ParseTransactions converts HAR 1.2 JSON into storage-ready APiX traffic.
func ParseTransactions(harJSON string) ([]*ImportedTransaction, error) {
	var doc archive
	if err := json.Unmarshal([]byte(harJSON), &doc); err != nil {
		return nil, fmt.Errorf("parse HAR: %w", err)
	}
	if doc.Log.Version == "" {
		return nil, fmt.Errorf("invalid HAR: missing log.version")
	}

	imported := make([]*ImportedTransaction, 0, len(doc.Log.Entries))
	for i, entry := range doc.Log.Entries {
		tx, err := parseEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		imported = append(imported, tx)
	}
	return imported, nil
}

func exportEntry(req *storage.RequestRecord, resp *storage.ResponseRecord) harEntry {
	requestHeaders := mapToHAR(req.Headers)
	requestBody := newHARBody(req.Body, headerValue(req.Headers, "Content-Type"))
	requestURL, _ := url.Parse(req.URL)

	response := harResponse{
		Status:      0,
		StatusText:  "",
		HTTPVersion: "HTTP/1.1",
		Headers:     []harKV{},
		Content:     harBody{Size: 0},
		RedirectURL: "",
		HeadersSize: -1,
		BodySize:    0,
	}

	if resp != nil {
		response = harResponse{
			Status:      resp.StatusCode,
			StatusText:  firstNonEmpty(resp.StatusText, http.StatusText(resp.StatusCode)),
			HTTPVersion: "HTTP/1.1",
			Headers:     mapToHAR(resp.Headers),
			Content:     newHARBody(resp.Body, headerValue(resp.Headers, "Content-Type")),
			RedirectURL: headerValue(resp.Headers, "Location"),
			HeadersSize: -1,
			BodySize:    len(resp.Body),
		}
	}

	return harEntry{
		StartedDateTime: req.Timestamp.UTC().Format(time.RFC3339Nano),
		Time:            float64(req.DurationMs),
		Request: harRequest{
			Method:      req.Method,
			URL:         req.URL,
			HTTPVersion: "HTTP/1.1",
			Headers:     requestHeaders,
			QueryString: queryString(requestURL),
			HeadersSize: -1,
			BodySize:    len(req.Body),
			PostData:    bodyPointer(requestBody, len(req.Body) > 0),
		},
		Response: response,
		Timings: harTimings{
			Send:    0,
			Wait:    float64(req.DurationMs),
			Receive: 0,
		},
	}
}

func parseEntry(entry harEntry) (*ImportedTransaction, error) {
	if entry.Request.URL == "" {
		return nil, fmt.Errorf("missing request.url")
	}

	timestamp := time.Now().UTC()
	if entry.StartedDateTime != "" {
		parsed, err := time.Parse(time.RFC3339Nano, entry.StartedDateTime)
		if err != nil {
			return nil, fmt.Errorf("parse startedDateTime: %w", err)
		}
		timestamp = parsed
	}

	id := uuid.NewString()
	reqHeaders := mapFromHAR(entry.Request.Headers)
	if entry.Request.PostData != nil && entry.Request.PostData.MimeType != "" {
		setMissingHeader(reqHeaders, "Content-Type", entry.Request.PostData.MimeType)
	}
	reqBody, err := decodeBody(entry.Request.PostData)
	if err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}

	tx := &ImportedTransaction{
		Request: &storage.RequestRecord{
			ID:         id,
			Method:     firstNonEmpty(entry.Request.Method, http.MethodGet),
			URL:        entry.Request.URL,
			Headers:    reqHeaders,
			Body:       reqBody,
			Timestamp:  timestamp,
			DurationMs: int64(entry.Time),
		},
	}

	if hasResponse(entry.Response) {
		respHeaders := mapFromHAR(entry.Response.Headers)
		if entry.Response.Content.MimeType != "" {
			setMissingHeader(respHeaders, "Content-Type", entry.Response.Content.MimeType)
		}
		respBody, err := decodeBody(&entry.Response.Content)
		if err != nil {
			return nil, fmt.Errorf("decode response body: %w", err)
		}
		tx.Response = &storage.ResponseRecord{
			RequestID:  id,
			StatusCode: entry.Response.Status,
			StatusText: firstNonEmpty(entry.Response.StatusText, http.StatusText(entry.Response.Status)),
			Headers:    respHeaders,
			Body:       respBody,
		}
	}

	return tx, nil
}

func hasResponse(resp harResponse) bool {
	return resp.Status > 0 || len(resp.Headers) > 0 || resp.Content.Text != "" || resp.StatusText != ""
}

func mapToHAR(headers map[string]string) []harKV {
	if len(headers) == 0 {
		return []harKV{}
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]harKV, 0, len(keys))
	for _, key := range keys {
		items = append(items, harKV{Name: key, Value: headers[key]})
	}
	return items
}

func mapFromHAR(headers []harKV) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(headers))
	for _, header := range headers {
		if header.Name == "" {
			continue
		}
		out[header.Name] = header.Value
	}
	return out
}

func queryString(parsed *url.URL) []harKV {
	if parsed == nil {
		return nil
	}
	values := parsed.Query()
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]harKV, 0)
	for _, key := range keys {
		for _, value := range values[key] {
			items = append(items, harKV{Name: key, Value: value})
		}
	}
	return items
}

func newHARBody(body []byte, mimeType string) harBody {
	result := harBody{Size: len(body), MimeType: mimeType}
	if len(body) == 0 {
		return result
	}
	if utf8.Valid(body) {
		result.Text = string(body)
		return result
	}
	result.Text = base64.StdEncoding.EncodeToString(body)
	result.Encoding = "base64"
	return result
}

func bodyPointer(body harBody, include bool) *harBody {
	if !include {
		return nil
	}
	copy := body
	return &copy
}

func decodeBody(body *harBody) ([]byte, error) {
	if body == nil || body.Text == "" {
		return nil, nil
	}
	if strings.EqualFold(body.Encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(body.Text)
		if err != nil {
			return nil, fmt.Errorf("base64 decode body: %w", err)
		}
		return decoded, nil
	}
	return []byte(body.Text), nil
}

func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func setMissingHeader(headers map[string]string, name, value string) {
	if value == "" {
		return
	}
	for key := range headers {
		if strings.EqualFold(key, name) {
			return
		}
	}
	headers[name] = value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
