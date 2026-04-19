package replay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/storage"
)

// ---------------------------------------------------------------------------
// engine.go: NewEngine with cfg.Client != nil
// ---------------------------------------------------------------------------

func TestNewEngine_WithCustomClient(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	custom := &http.Client{Timeout: 5 * time.Second}
	eng := NewEngine(db, &ClientConfig{Client: custom})
	if eng == nil {
		t.Fatal("expected non-nil engine")
	}
	// Verify the custom client is used by checking it successfully issues a
	// request through a test server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	rawReq, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{RawRequest: rawReq})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d want 204", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// engine.go: NewEngine with individual ClientConfig timeout fields
// ---------------------------------------------------------------------------

func TestNewEngine_WithCustomTimeouts(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	eng := NewEngine(db, &ClientConfig{
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		ExpectContinueTimeout: 500 * time.Millisecond,
		MaxIdleConnsPerHost:   5,
	})
	if eng == nil {
		t.Fatal("expected non-nil engine")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rawReq, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{RawRequest: rawReq})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// engine.go: ReplayRequest with neither RequestID nor RawRequest (built=false)
// ---------------------------------------------------------------------------

func TestReplayRequest_NeitherRequestIDNorRaw(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	eng := NewEngine(db, nil)

	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{})
	if err == nil {
		t.Fatal("expected error when neither RequestID nor RawRequest is set")
	}
	if !strings.Contains(err.Error(), "RequestID") && !strings.Contains(err.Error(), "RawRequest") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// filter.go: ShouldRecord on a nil *RecordingFilter
// ---------------------------------------------------------------------------

func TestShouldRecord_NilRecordingFilter(t *testing.T) {
	t.Parallel()
	var f *RecordingFilter
	req, _ := http.NewRequest("GET", "http://example.com/anything", nil)
	if !f.ShouldRecord(req) {
		t.Error("nil RecordingFilter should record everything")
	}
}

// ---------------------------------------------------------------------------
// filter.go: ShouldRecord host derived from req.URL when req.Host is empty
// ---------------------------------------------------------------------------

func TestShouldRecord_HostDerivedFromURL(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{IncludeHosts: []string{"api.example.com"}}

	// req.Host is deliberately left empty — host must be resolved from req.URL.
	req, _ := http.NewRequest("GET", "http://api.example.com/v1/data", nil)
	// Ensure req.Host is empty (default from http.NewRequest).
	req.Host = ""

	if !f.ShouldRecord(req) {
		t.Error("request to api.example.com should be recorded")
	}

	other, _ := http.NewRequest("GET", "http://other.example.com/v1/data", nil)
	other.Host = ""
	if f.ShouldRecord(other) {
		t.Error("request to other.example.com should not be recorded")
	}
}

// ---------------------------------------------------------------------------
// sources.go: storedRequestBuilder – DB GetTransaction returns an error
// ---------------------------------------------------------------------------

func TestStoredBuilder_DBError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	eng := NewEngine(db, nil)

	// Close the DB so GetTransaction fails with an error.
	db.Close()

	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID: "any-id",
	})
	if err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

// ---------------------------------------------------------------------------
// sources.go: storedRequestBuilder – OverrideBody replaces the stored body
// ---------------------------------------------------------------------------

func TestStoredBuilder_OverrideBodyReplacesStoredBody(t *testing.T) {
	t.Parallel()
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "stored-body-req",
		Method: "POST",
		URL:    srv.URL + "/endpoint",
		Body:   []byte("original-stored-body"),
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	eng := NewEngine(db, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID:    "stored-body-req",
		OverrideBody: []byte("override-body"),
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedBody != "override-body" {
		t.Errorf("body: got %q want %q", receivedBody, "override-body")
	}
}

// ---------------------------------------------------------------------------
// sources.go: storedRequestBuilder – stored rec.Body is used when no override
// ---------------------------------------------------------------------------

func TestStoredBuilder_UsesRecordBody(t *testing.T) {
	t.Parallel()
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "rec-body-req",
		Method: "POST",
		URL:    srv.URL + "/endpoint",
		Body:   []byte("stored-body-data"),
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	eng := NewEngine(db, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID: "rec-body-req",
		// No OverrideBody — should send the stored body.
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedBody != "stored-body-data" {
		t.Errorf("body: got %q want %q", receivedBody, "stored-body-data")
	}
}

// ---------------------------------------------------------------------------
// sources.go: storedRequestBuilder – http.NewRequestWithContext fails (bad URL)
// ---------------------------------------------------------------------------

func TestStoredBuilder_InvalidStoredURL(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Store a request with a URL that http.NewRequestWithContext will reject.
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "bad-url-req",
		Method: "GET",
		URL:    "://invalid-url",
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	eng := NewEngine(db, nil)
	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID: "bad-url-req",
	})
	if err == nil {
		t.Fatal("expected error for stored request with invalid URL")
	}
}

// ---------------------------------------------------------------------------
// sources.go: rawRequestBuilder – io.ReadAll error on Body
// ---------------------------------------------------------------------------

// errReader always returns an error on Read, simulating a broken body.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }

func TestRawBuilder_BodyReadError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	eng := NewEngine(db, nil)

	rawReq, _ := http.NewRequest("POST", "http://127.0.0.1:1/", nil)
	rawReq.Body = io.NopCloser(errReader{})

	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest: rawReq,
	})
	if err == nil {
		t.Fatal("expected error when body read fails")
	}
}

// ---------------------------------------------------------------------------
// sources.go: rawRequestBuilder – http.NewRequestWithContext fails (bad URL)
// ---------------------------------------------------------------------------

func TestRawBuilder_InvalidURL(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	eng := NewEngine(db, nil)

	rawReq, _ := http.NewRequest("GET", "http://example.com", nil)
	// Override the URL to something NewRequestWithContext rejects.
	rawReq.URL.Scheme = "://bad"
	rawReq.URL.Host = ""

	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest: rawReq,
	})
	if err == nil {
		t.Fatal("expected error for raw request with invalid URL")
	}
}

// ---------------------------------------------------------------------------
// engine.go: context cancellation mid-request (context timeout)
// ---------------------------------------------------------------------------

func TestReplayRequest_ContextCancelled(t *testing.T) {
	t.Parallel()

	// Server that hangs until the client disconnects.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	eng := NewEngine(db, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	rawReq, _ := http.NewRequest("GET", srv.URL, nil)
	_, err := eng.ReplayRequest(ctx, &ReplayRequest{RawRequest: rawReq})
	if err == nil {
		t.Fatal("expected error when context is cancelled mid-request")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context") {
		t.Logf("got error (expected context-related): %v", err)
	}
}

// ---------------------------------------------------------------------------
// engine.go: OverrideMethod applied via RequestID path
// ---------------------------------------------------------------------------

func TestStoredBuilder_OverrideMethodAndHeaders(t *testing.T) {
	t.Parallel()
	var receivedMethod, receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "override-combo-req",
		Method: "GET",
		URL:    srv.URL + "/resource",
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	eng := NewEngine(db, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID:       "override-combo-req",
		OverrideMethod:  "DELETE",
		OverrideHeaders: map[string]string{"X-Custom": "test-value"},
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedMethod != "DELETE" {
		t.Errorf("method: got %q want DELETE", receivedMethod)
	}
	if receivedHeader != "test-value" {
		t.Errorf("X-Custom: got %q want test-value", receivedHeader)
	}
}

// ---------------------------------------------------------------------------
// Large payload via RawRequest path
// ---------------------------------------------------------------------------

func TestRawBuilder_LargeOverrideBody(t *testing.T) {
	t.Parallel()
	const size = 2 << 20 // 2 MiB
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = 'z'
	}

	var receivedLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedLen = len(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	eng := NewEngine(db, nil)

	rawReq, _ := http.NewRequest("PUT", srv.URL, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:   rawReq,
		OverrideBody: payload,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedLen != size {
		t.Errorf("received len: got %d want %d", receivedLen, size)
	}
}
