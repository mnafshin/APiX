package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAndClose(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestSaveAndGetRequest(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now().Truncate(time.Millisecond)
	req := &RequestRecord{
		ID:         "req-1",
		Method:     "GET",
		URL:        "https://example.com/api",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte("hello"),
		Timestamp:  now,
		DurationMs: 42,
	}

	if err := db.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	gotReq, gotResp, err := db.GetTransaction("req-1")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if gotReq == nil {
		t.Fatal("expected request, got nil")
	}
	if gotResp != nil {
		t.Fatal("expected no response, got one")
	}
	if gotReq.ID != req.ID {
		t.Errorf("ID: got %q want %q", gotReq.ID, req.ID)
	}
	if gotReq.Method != req.Method {
		t.Errorf("Method: got %q want %q", gotReq.Method, req.Method)
	}
	if gotReq.URL != req.URL {
		t.Errorf("URL: got %q want %q", gotReq.URL, req.URL)
	}
	if gotReq.Headers["Content-Type"] != "application/json" {
		t.Errorf("Headers: got %v", gotReq.Headers)
	}
	if string(gotReq.Body) != "hello" {
		t.Errorf("Body: got %q want %q", gotReq.Body, "hello")
	}
	if gotReq.DurationMs != 42 {
		t.Errorf("DurationMs: got %d want 42", gotReq.DurationMs)
	}
	if !gotReq.Timestamp.Equal(now) {
		t.Errorf("Timestamp: got %v want %v", gotReq.Timestamp, now)
	}
}

func TestSaveAndGetResponse(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	req := &RequestRecord{
		ID:        "req-2",
		Method:    "POST",
		URL:       "https://api.example.com/data",
		Headers:   map[string]string{},
		Timestamp: time.Now(),
	}
	if err := db.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	resp := &ResponseRecord{
		RequestID:  "req-2",
		StatusCode: 200,
		StatusText: "OK",
		Headers:    map[string]string{"X-Custom": "value"},
		Body:       []byte(`{"ok":true}`),
	}
	if err := db.SaveResponse(resp); err != nil {
		t.Fatalf("SaveResponse: %v", err)
	}

	gotReq, gotResp, err := db.GetTransaction("req-2")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if gotReq == nil || gotResp == nil {
		t.Fatal("expected both request and response, got nil")
	}
	if gotResp.RequestID != "req-2" {
		t.Errorf("RequestID: got %q", gotResp.RequestID)
	}
	if gotResp.StatusCode != 200 {
		t.Errorf("StatusCode: got %d", gotResp.StatusCode)
	}
	if gotResp.StatusText != "OK" {
		t.Errorf("StatusText: got %q", gotResp.StatusText)
	}
	if gotResp.Headers["X-Custom"] != "value" {
		t.Errorf("Headers: got %v", gotResp.Headers)
	}
	if string(gotResp.Body) != `{"ok":true}` {
		t.Errorf("Body: got %q", gotResp.Body)
	}
}

func makeRequest(id, method, url string) *RequestRecord {
	return &RequestRecord{
		ID:        id,
		Method:    method,
		URL:       url,
		Headers:   map[string]string{},
		Timestamp: time.Now(),
	}
}

func TestListTransactions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	records := []struct {
		req    *RequestRecord
		status int
	}{
		{makeRequest("r1", "GET", "https://example.com/a"), 200},
		{makeRequest("r2", "POST", "https://example.com/b"), 404},
		{makeRequest("r3", "GET", "https://other.com/c"), 200},
		{makeRequest("r4", "DELETE", "https://example.com/d"), 500},
		{makeRequest("r5", "GET", "https://example.com/e"), 201},
	}
	for _, rec := range records {
		if err := db.SaveRequest(rec.req); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
		if err := db.SaveResponse(&ResponseRecord{
			RequestID:  rec.req.ID,
			StatusCode: rec.status,
			StatusText: "status",
			Headers:    map[string]string{},
		}); err != nil {
			t.Fatalf("SaveResponse: %v", err)
		}
	}

	t.Run("limit", func(t *testing.T) {
		reqs, _, err := db.ListTransactions(2, 0, "", "", 0)
		if err != nil {
			t.Fatalf("ListTransactions: %v", err)
		}
		if len(reqs) != 2 {
			t.Errorf("expected 2 results, got %d", len(reqs))
		}
	})

	t.Run("offset", func(t *testing.T) {
		reqs, _, err := db.ListTransactions(100, 3, "", "", 0)
		if err != nil {
			t.Fatalf("ListTransactions: %v", err)
		}
		if len(reqs) != 2 {
			t.Errorf("expected 2 results after offset 3, got %d", len(reqs))
		}
	})

	t.Run("url_filter", func(t *testing.T) {
		reqs, _, err := db.ListTransactions(100, 0, "example.com", "", 0)
		if err != nil {
			t.Fatalf("ListTransactions: %v", err)
		}
		if len(reqs) != 4 {
			t.Errorf("expected 4 example.com results, got %d", len(reqs))
		}
	})

	t.Run("method_filter", func(t *testing.T) {
		reqs, _, err := db.ListTransactions(100, 0, "", "GET", 0)
		if err != nil {
			t.Fatalf("ListTransactions: %v", err)
		}
		if len(reqs) != 3 {
			t.Errorf("expected 3 GET results, got %d", len(reqs))
		}
	})

	t.Run("status_filter", func(t *testing.T) {
		reqs, _, err := db.ListTransactions(100, 0, "", "", 200)
		if err != nil {
			t.Fatalf("ListTransactions: %v", err)
		}
		if len(reqs) != 2 {
			t.Errorf("expected 2 results with status 200, got %d", len(reqs))
		}
	})
}

func TestDeleteAllTransactions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	for i, id := range []string{"x1", "x2", "x3"} {
		_ = i
		if err := db.SaveRequest(makeRequest(id, "GET", "https://example.com")); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
	}

	if err := db.DeleteAllTransactions(); err != nil {
		t.Fatalf("DeleteAllTransactions: %v", err)
	}

	reqs, _, err := db.ListTransactions(100, 0, "", "", 0)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(reqs))
	}
}

func TestSaveAndListBreakpoints(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	bps := []struct {
		id, pattern, label string
		methods             []string
		enabled             bool
	}{
		{"bp-1", ".*example\\.com.*", "Test BP 1", []string{"GET", "POST"}, true},
		{"bp-2", ".*api\\..*", "Test BP 2", []string{"DELETE"}, false},
		{"bp-3", ".*", "Catch-all", nil, true},
	}

	for _, bp := range bps {
		if err := db.SaveBreakpoint(bp.id, bp.pattern, bp.methods, bp.enabled, bp.label); err != nil {
			t.Fatalf("SaveBreakpoint %q: %v", bp.id, err)
		}
	}

	list, err := db.ListBreakpoints()
	if err != nil {
		t.Fatalf("ListBreakpoints: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 breakpoints, got %d", len(list))
	}

	byID := make(map[string]*BreakpointRecord)
	for _, bp := range list {
		byID[bp.ID] = bp
	}

	bp1 := byID["bp-1"]
	if bp1 == nil {
		t.Fatal("bp-1 not found")
	}
	if bp1.URLPattern != ".*example\\.com.*" {
		t.Errorf("URLPattern: got %q", bp1.URLPattern)
	}
	if bp1.Label != "Test BP 1" {
		t.Errorf("Label: got %q", bp1.Label)
	}
	if !bp1.Enabled {
		t.Error("expected bp-1 to be enabled")
	}
	if len(bp1.Methods) != 2 {
		t.Errorf("Methods: got %v", bp1.Methods)
	}

	bp2 := byID["bp-2"]
	if bp2 == nil {
		t.Fatal("bp-2 not found")
	}
	if bp2.Enabled {
		t.Error("expected bp-2 to be disabled")
	}
}

func TestDeleteBreakpoint(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	for _, id := range []string{"d1", "d2"} {
		if err := db.SaveBreakpoint(id, ".*", nil, true, ""); err != nil {
			t.Fatalf("SaveBreakpoint: %v", err)
		}
	}

	if err := db.DeleteBreakpoint("d1"); err != nil {
		t.Fatalf("DeleteBreakpoint: %v", err)
	}

	list, err := db.ListBreakpoints()
	if err != nil {
		t.Fatalf("ListBreakpoints: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 breakpoint, got %d", len(list))
	}
	if list[0].ID != "d2" {
		t.Errorf("expected d2 to remain, got %q", list[0].ID)
	}
}

func TestForeignKeyCascade(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	req := makeRequest("cascade-req", "GET", "https://example.com")
	if err := db.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	if err := db.SaveResponse(&ResponseRecord{
		RequestID:  "cascade-req",
		StatusCode: 200,
		StatusText: "OK",
		Headers:    map[string]string{},
	}); err != nil {
		t.Fatalf("SaveResponse: %v", err)
	}

	// Verify response exists.
	gotReq, gotResp, err := db.GetTransaction("cascade-req")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if gotReq == nil || gotResp == nil {
		t.Fatal("expected request and response before cascade")
	}

	// Delete via DeleteAllTransactions which cascades.
	if err := db.DeleteAllTransactions(); err != nil {
		t.Fatalf("DeleteAllTransactions: %v", err)
	}

	// After delete, both should be gone.
	gotReq, gotResp, err = db.GetTransaction("cascade-req")
	if err != nil {
		t.Fatalf("GetTransaction after delete: %v", err)
	}
	if gotReq != nil || gotResp != nil {
		t.Error("expected both request and response to be deleted (cascade)")
	}
}
