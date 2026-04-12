package storage

import (
	"fmt"
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

func TestSaveAndListWebSocketFrames(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	req := makeRequest("ws-req-1", "GET", "wss://example.com/socket")
	if err := db.SaveRequest(req); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	frames := []*WebSocketFrameRecord{
		{
			TransactionID: "ws-req-1",
			Direction:     "client",
			Opcode:        1,
			Payload:       []byte("hello"),
			Timestamp:     time.Unix(1700000000, 0).UTC(),
		},
		{
			TransactionID: "ws-req-1",
			Direction:     "server",
			Opcode:        2,
			Payload:       []byte{0x00, 0x01, 0x02},
			Timestamp:     time.Unix(1700000001, 0).UTC(),
		},
		{
			TransactionID: "ws-req-1",
			Direction:     "server",
			Opcode:        9,
			Payload:       nil,
			Timestamp:     time.Unix(1700000002, 0).UTC(),
		},
	}
	for _, frame := range frames {
		if err := db.SaveWebSocketFrame(frame); err != nil {
			t.Fatalf("SaveWebSocketFrame: %v", err)
		}
		if frame.ID == "" {
			t.Fatal("expected frame ID to be assigned")
		}
	}

	got, err := db.ListWebSocketFrames("ws-req-1")
	if err != nil {
		t.Fatalf("ListWebSocketFrames: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 websocket frames, got %d", len(got))
	}
	if got[0].Direction != "client" || string(got[0].Payload) != "hello" {
		t.Fatalf("first frame mismatch: %+v", got[0])
	}
	if got[1].Opcode != 2 || string(got[1].Payload) != string([]byte{0x00, 0x01, 0x02}) {
		t.Fatalf("second frame mismatch: %+v", got[1])
	}
	if got[2].Opcode != 9 || len(got[2].Payload) != 0 {
		t.Fatalf("third frame mismatch: %+v", got[2])
	}
}

func TestSaveAndListBreakpoints(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	bps := []struct {
		id, pattern, label string
		methods            []string
		enabled            bool
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
	if err := db.SaveWebSocketFrame(&WebSocketFrameRecord{
		TransactionID: "cascade-req",
		Direction:     "client",
		Opcode:        1,
		Payload:       []byte("hello"),
		Timestamp:     time.Now(),
	}); err != nil {
		t.Fatalf("SaveWebSocketFrame: %v", err)
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
	frames, err := db.ListWebSocketFrames("cascade-req")
	if err != nil {
		t.Fatalf("ListWebSocketFrames after delete: %v", err)
	}
	if len(frames) != 0 {
		t.Errorf("expected websocket frames to cascade delete, got %d", len(frames))
	}
}

// ── Pagination ──────────────────────────────────────────────────────────────

func TestListTransactions_Pagination(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Seed 15 requests with distinct timestamps so ordering is deterministic.
	base := time.Now().Add(-15 * time.Second)
	for i := 0; i < 15; i++ {
		if err := db.SaveRequest(&RequestRecord{
			ID:        fmt.Sprintf("pag-%02d", i),
			Method:    "GET",
			URL:       "https://example.com/",
			Headers:   map[string]string{},
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
	}

	// Page 1: limit=5, offset=0 → 5 records.
	page1, _, err := db.ListTransactions(5, 0, "", "", 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 5 {
		t.Errorf("page 1: got %d want 5", len(page1))
	}

	// Page 2: limit=5, offset=5 → 5 records.
	page2, _, err := db.ListTransactions(5, 5, "", "", 0)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 5 {
		t.Errorf("page 2: got %d want 5", len(page2))
	}

	// Pages must not overlap.
	ids1 := make(map[string]struct{})
	for _, r := range page1 {
		ids1[r.ID] = struct{}{}
	}
	for _, r := range page2 {
		if _, dup := ids1[r.ID]; dup {
			t.Errorf("pages overlap at ID %q", r.ID)
		}
	}

	// Page 3: limit=5, offset=10 → remaining 5.
	page3, _, err := db.ListTransactions(5, 10, "", "", 0)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 5 {
		t.Errorf("page 3: got %d want 5", len(page3))
	}
}

// ── Duplicate ID ────────────────────────────────────────────────────────────

func TestSaveRequest_DuplicateID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	rec := &RequestRecord{
		ID:        "dup-id",
		Method:    "GET",
		URL:       "https://example.com/",
		Headers:   map[string]string{},
		Timestamp: time.Now(),
	}
	if err := db.SaveRequest(rec); err != nil {
		t.Fatalf("first SaveRequest: %v", err)
	}

	// SaveRequest uses INSERT OR REPLACE (upsert), so a duplicate should update
	// the record, not return an error.
	rec.Method = "POST"
	rec.URL = "https://example.com/updated"
	if err := db.SaveRequest(rec); err != nil {
		t.Fatalf("upsert SaveRequest: %v", err)
	}

	got, _, err := db.GetTransaction("dup-id")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Method != "POST" || got.URL != "https://example.com/updated" {
		t.Errorf("after upsert: method=%q url=%q", got.Method, got.URL)
	}
}

// ── SaveBreakpoint – upsert ─────────────────────────────────────────────────

func TestSaveBreakpoint_UpdateExisting(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	const id = "bp-upsert"
	if err := db.SaveBreakpoint(id, "https://api.example.com/.*", nil, true, "original"); err != nil {
		t.Fatalf("first SaveBreakpoint: %v", err)
	}

	// Update the same ID with a different label and disabled state.
	if err := db.SaveBreakpoint(id, "https://api.example.com/.*", nil, false, "updated"); err != nil {
		t.Fatalf("upsert SaveBreakpoint: %v", err)
	}

	bps, err := db.ListBreakpoints()
	if err != nil {
		t.Fatalf("ListBreakpoints: %v", err)
	}
	var found *BreakpointRecord
	for _, bp := range bps {
		if bp.ID == id {
			found = bp
			break
		}
	}
	if found == nil {
		t.Fatalf("breakpoint %q not found after upsert", id)
	}
	if found.Enabled {
		t.Error("Enabled: expected false after upsert")
	}
	if found.Label != "updated" {
		t.Errorf("Label: got %q want updated", found.Label)
	}
}

// ── ListTransactions – filter combinations ──────────────────────────────────

func TestListTransactions_MethodFilter(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	for _, rec := range []*RequestRecord{
		{ID: "get-1", Method: "GET", URL: "https://a.com/", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "post-1", Method: "POST", URL: "https://a.com/create", Headers: map[string]string{}, Timestamp: time.Now()},
		{ID: "post-2", Method: "POST", URL: "https://b.com/submit", Headers: map[string]string{}, Timestamp: time.Now()},
	} {
		if err := db.SaveRequest(rec); err != nil {
			t.Fatalf("SaveRequest: %v", err)
		}
	}

	results, _, err := db.ListTransactions(10, 0, "", "POST", 0)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("method filter POST: got %d want 2", len(results))
	}
	for _, r := range results {
		if r.Method != "POST" {
			t.Errorf("expected only POST, got %q (ID=%s)", r.Method, r.ID)
		}
	}
}

// ── Benchmark: Storage Write Rate ─────────────────────────────────────────

func BenchmarkStorage_SaveRequest(b *testing.B) {
	db, err := Open(":memory:")
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reqRecord := &RequestRecord{
			ID:        fmt.Sprintf("bench-req-%d", i),
			Method:    "GET",
			URL:       fmt.Sprintf("http://example.com/bench/%d", i),
			Headers:   map[string]string{"Content-Type": "application/json"},
			Body:      []byte("test body"),
			Timestamp: time.Now(),
		}
		if err := db.SaveRequest(reqRecord); err != nil {
			b.Fatalf("SaveRequest: %v", err)
		}
	}
}

func BenchmarkStorage_ListTransactions(b *testing.B) {
	db, err := Open(":memory:")
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Seed 1000 rows
	for i := 0; i < 1000; i++ {
		reqRecord := &RequestRecord{
			ID:        fmt.Sprintf("seed-req-%d", i),
			Method:    "GET",
			URL:       fmt.Sprintf("http://example.com/bench/%d", i),
			Headers:   map[string]string{},
			Body:      []byte("test"),
			Timestamp: time.Now(),
		}
		if err := db.SaveRequest(reqRecord); err != nil {
			b.Fatalf("SaveRequest: %v", err)
		}

		respRecord := &ResponseRecord{
			RequestID:  reqRecord.ID,
			StatusCode: 200,
			StatusText: "OK",
			Headers:    map[string]string{},
			Body:       []byte("response"),
		}
		if err := db.SaveResponse(respRecord); err != nil {
			b.Fatalf("SaveResponse: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.ListTransactions(100, 0, "", "", 0)
		if err != nil {
			b.Fatalf("ListTransactions: %v", err)
		}
	}
}
