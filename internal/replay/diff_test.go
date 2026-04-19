package replay

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/storage"
)

// ---------------------------------------------------------------------------
// DiffResponses
// ---------------------------------------------------------------------------

func makeTransaction(statusCode int, headers map[string]string, body []byte) *storage.Transaction {
	resp := &storage.ResponseRecord{
		RequestID:  "test-id",
		StatusCode: statusCode,
		StatusText: http.StatusText(statusCode),
		Headers:    headers,
		Body:       body,
	}
	return &storage.Transaction{
		Request: &storage.RequestRecord{
			ID:        "test-id",
			Method:    "GET",
			URL:       "http://example.com/",
			Timestamp: time.Now(),
		},
		Response: resp,
	}
}

func makeHTTPResponse(statusCode int, headers map[string]string, body []byte) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	contentLength := int64(-1)
	var bodyReader io.ReadCloser = http.NoBody
	if body != nil {
		contentLength = int64(len(body))
		bodyReader = io.NopCloser(bytes.NewReader(body))
	}
	return &http.Response{
		StatusCode:    statusCode,
		Header:        h,
		Body:          bodyReader,
		ContentLength: contentLength,
	}
}

func TestDiffResponses_StatusMatch(t *testing.T) {
	t.Parallel()
	orig := makeTransaction(200, nil, nil)
	rep := makeHTTPResponse(200, nil, nil)

	d := DiffResponses(orig, rep)
	if !d.StatusMatch {
		t.Error("expected StatusMatch=true")
	}
	if d.StatusOriginal != 200 || d.StatusReplayed != 200 {
		t.Errorf("status: orig=%d rep=%d", d.StatusOriginal, d.StatusReplayed)
	}
}

func TestDiffResponses_StatusMismatch(t *testing.T) {
	t.Parallel()
	orig := makeTransaction(200, nil, nil)
	rep := makeHTTPResponse(500, nil, nil)

	d := DiffResponses(orig, rep)
	if d.StatusMatch {
		t.Error("expected StatusMatch=false for 200 vs 500")
	}
	if d.StatusOriginal != 200 {
		t.Errorf("StatusOriginal: got %d want 200", d.StatusOriginal)
	}
	if d.StatusReplayed != 500 {
		t.Errorf("StatusReplayed: got %d want 500", d.StatusReplayed)
	}
}

func TestDiffResponses_HeaderDiff_ValueChanged(t *testing.T) {
	t.Parallel()
	orig := makeTransaction(200, map[string]string{"content-type": "application/json"}, nil)
	rep := makeHTTPResponse(200, map[string]string{"Content-Type": "text/plain"}, nil)

	d := DiffResponses(orig, rep)
	if len(d.HeaderDiffs) == 0 {
		t.Fatal("expected header diffs, got none")
	}
	found := false
	for _, hd := range d.HeaderDiffs {
		if hd.Name == "content-type" {
			found = true
			if hd.Original != "application/json" {
				t.Errorf("Original: got %q want application/json", hd.Original)
			}
			if hd.Replayed != "text/plain" {
				t.Errorf("Replayed: got %q want text/plain", hd.Replayed)
			}
		}
	}
	if !found {
		t.Error("content-type diff not reported")
	}
}

func TestDiffResponses_HeaderDiff_NewHeaderInReplay(t *testing.T) {
	t.Parallel()
	orig := makeTransaction(200, map[string]string{}, nil)
	rep := makeHTTPResponse(200, map[string]string{"X-New": "value"}, nil)

	d := DiffResponses(orig, rep)
	found := false
	for _, hd := range d.HeaderDiffs {
		if hd.Name == "x-new" && hd.Original == "" && hd.Replayed == "value" {
			found = true
		}
	}
	if !found {
		t.Error("new header in replay not reported as diff")
	}
}

func TestDiffResponses_BodySizeMatch(t *testing.T) {
	t.Parallel()
	body := []byte("hello")
	orig := makeTransaction(200, nil, body)
	rep := makeHTTPResponse(200, nil, body)

	d := DiffResponses(orig, rep)
	if !d.BodyMatch {
		t.Errorf("expected BodyMatch=true; orig=%d rep=%d", d.BodySizeOriginal, d.BodySizeReplayed)
	}
}

func TestDiffResponses_BodySizeMismatch(t *testing.T) {
	t.Parallel()
	orig := makeTransaction(200, nil, []byte("hello world"))
	rep := makeHTTPResponse(200, nil, []byte("hi"))

	d := DiffResponses(orig, rep)
	if d.BodyMatch {
		t.Error("expected BodyMatch=false for different body sizes")
	}
	if d.BodySizeOriginal != 11 {
		t.Errorf("BodySizeOriginal: got %d want 11", d.BodySizeOriginal)
	}
	if d.BodySizeReplayed != 2 {
		t.Errorf("BodySizeReplayed: got %d want 2", d.BodySizeReplayed)
	}
}

func TestDiffResponses_NilOriginal(t *testing.T) {
	t.Parallel()
	rep := makeHTTPResponse(200, nil, nil)
	d := DiffResponses(nil, rep)
	// Should not panic; all match fields should be false.
	if d.StatusMatch {
		t.Error("StatusMatch should be false with nil original")
	}
}

func TestDiffResponses_NilOriginalResponse(t *testing.T) {
	t.Parallel()
	orig := &storage.Transaction{
		Request: &storage.RequestRecord{ID: "x", Method: "GET", URL: "http://x.com/"},
		// Response intentionally nil
	}
	rep := makeHTTPResponse(201, nil, nil)
	d := DiffResponses(orig, rep)
	if d.StatusMatch {
		t.Error("StatusMatch should be false when original has no response")
	}
}

// ---------------------------------------------------------------------------
// ExportScenario / ImportScenario round-trip
// ---------------------------------------------------------------------------

func TestExportImportScenario_RoundTrip(t *testing.T) {
	t.Parallel()
	original := Scenario{
		Name: "my-scenario",
		Requests: []ScenarioStep{
			{
				RequestID:       "req-1",
				DelayBefore:     100 * time.Millisecond,
				OverrideHeaders: map[string]string{"X-Test": "value"},
			},
			{
				RequestID: "req-2",
			},
		},
	}

	data, err := ExportScenario(original)
	if err != nil {
		t.Fatalf("ExportScenario: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ExportScenario returned empty bytes")
	}

	imported, err := ImportScenario(data)
	if err != nil {
		t.Fatalf("ImportScenario: %v", err)
	}

	if imported.Name != original.Name {
		t.Errorf("Name: got %q want %q", imported.Name, original.Name)
	}
	if len(imported.Requests) != len(original.Requests) {
		t.Fatalf("Requests length: got %d want %d", len(imported.Requests), len(original.Requests))
	}

	step0 := imported.Requests[0]
	if step0.RequestID != "req-1" {
		t.Errorf("step0.RequestID: got %q want req-1", step0.RequestID)
	}
	if step0.DelayBefore != 100*time.Millisecond {
		t.Errorf("step0.DelayBefore: got %v want 100ms", step0.DelayBefore)
	}
	if step0.OverrideHeaders["X-Test"] != "value" {
		t.Errorf("step0.OverrideHeaders[X-Test]: got %q want value", step0.OverrideHeaders["X-Test"])
	}
}

func TestImportScenario_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := ImportScenario([]byte("not-json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
