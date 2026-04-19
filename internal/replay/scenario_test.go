package replay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func seedRequest(t *testing.T, db *storage.DB, id, method, rawURL string) {
	t.Helper()
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:        id,
		Method:    method,
		URL:       rawURL,
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RunScenario
// ---------------------------------------------------------------------------

func TestRunScenario_OrderAndResults(t *testing.T) {
	t.Parallel()

	var callOrder []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callOrder = append(callOrder, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	seedRequest(t, db, "step-1", "GET", srv.URL+"/first")
	seedRequest(t, db, "step-2", "GET", srv.URL+"/second")
	seedRequest(t, db, "step-3", "GET", srv.URL+"/third")

	eng := NewEngine(db, nil)
	s := Scenario{
		Name: "ordered",
		Requests: []ScenarioStep{
			{RequestID: "step-1"},
			{RequestID: "step-2"},
			{RequestID: "step-3"},
		},
	}

	results := eng.RunScenario(context.Background(), s)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Error != "" {
			t.Errorf("step %d: unexpected error: %s", i, r.Error)
		}
		if r.StatusCode != http.StatusOK {
			t.Errorf("step %d: got status %d, want 200", i, r.StatusCode)
		}
		if r.StepIndex != i {
			t.Errorf("step %d: StepIndex=%d", i, r.StepIndex)
		}
	}

	wantOrder := []string{"/first", "/second", "/third"}
	for i, path := range callOrder {
		if path != wantOrder[i] {
			t.Errorf("call %d: got path %q want %q", i, path, wantOrder[i])
		}
	}
}

func TestRunScenario_StepFailureDoesNotAbort(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	seedRequest(t, db, "ok-step", "GET", srv.URL+"/ok")
	// "missing-step" is intentionally not seeded so the lookup will fail.

	eng := NewEngine(db, nil)
	s := Scenario{
		Name: "partial-fail",
		Requests: []ScenarioStep{
			{RequestID: "ok-step"},
			{RequestID: "missing-step"},
			{RequestID: "ok-step"},
		},
	}

	results := eng.RunScenario(context.Background(), s)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Errorf("step 0: unexpected error: %s", results[0].Error)
	}
	if results[1].Error == "" {
		t.Error("step 1: expected error for missing request, got none")
	}
	if results[2].Error != "" {
		t.Errorf("step 2: unexpected error after failed step: %s", results[2].Error)
	}
}

func TestRunScenario_RespectsDelay(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	seedRequest(t, db, "delay-req", "GET", srv.URL+"/")

	eng := NewEngine(db, nil)
	delay := 50 * time.Millisecond
	s := Scenario{
		Name: "delay",
		Requests: []ScenarioStep{
			{RequestID: "delay-req", DelayBefore: delay},
		},
	}

	start := time.Now()
	results := eng.RunScenario(context.Background(), s)
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Errorf("unexpected error: %s", results[0].Error)
	}
	if elapsed < delay {
		t.Errorf("elapsed %v < delay %v: step delay was not honoured", elapsed, delay)
	}
}

func TestRunScenario_ContextCancellation(t *testing.T) {
	t.Parallel()

	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	seedRequest(t, db, "ctx-req", "GET", srv.URL+"/")

	eng := NewEngine(db, nil)
	// 500 ms delay on step 1 ensures cancellation fires before step 1 runs.
	s := Scenario{
		Name: "cancel",
		Requests: []ScenarioStep{
			{RequestID: "ctx-req"},
			{RequestID: "ctx-req", DelayBefore: 500 * time.Millisecond},
			{RequestID: "ctx-req"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := eng.RunScenario(ctx, s)

	// Step 0 completes, step 1 should be aborted during its delay.
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}
	last := results[len(results)-1]
	if last.Error == "" {
		t.Error("expected a context cancellation error in last result")
	}
}

// ---------------------------------------------------------------------------
// RecordingFilter.ShouldRecord
// ---------------------------------------------------------------------------

func TestRecordingFilter_ShouldRecord_NoRules(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{}
	req, _ := http.NewRequest("GET", "http://example.com/anything", nil)
	if !f.ShouldRecord(req) {
		t.Error("empty filter should record everything")
	}
}

func TestRecordingFilter_ShouldRecord_IncludePaths(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{IncludePaths: []string{"/api/"}}

	apiReq, _ := http.NewRequest("GET", "http://example.com/api/users", nil)
	otherReq, _ := http.NewRequest("GET", "http://example.com/static/app.js", nil)

	if !f.ShouldRecord(apiReq) {
		t.Error("/api/users should be recorded")
	}
	if f.ShouldRecord(otherReq) {
		t.Error("/static/app.js should not be recorded when IncludePaths=[/api/]")
	}
}

func TestRecordingFilter_ShouldRecord_ExcludePaths(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{ExcludePaths: []string{"/health", "/metrics"}}

	apiReq, _ := http.NewRequest("GET", "http://example.com/api/data", nil)
	healthReq, _ := http.NewRequest("GET", "http://example.com/health", nil)

	if !f.ShouldRecord(apiReq) {
		t.Error("/api/data should be recorded")
	}
	if f.ShouldRecord(healthReq) {
		t.Error("/health should be excluded")
	}
}

func TestRecordingFilter_ShouldRecord_ExcludeOverridesInclude(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{
		IncludePaths: []string{"/api/"},
		ExcludePaths: []string{"/api/internal"},
	}

	pubReq, _ := http.NewRequest("GET", "http://example.com/api/public", nil)
	intReq, _ := http.NewRequest("GET", "http://example.com/api/internal/secret", nil)

	if !f.ShouldRecord(pubReq) {
		t.Error("/api/public should be recorded")
	}
	if f.ShouldRecord(intReq) {
		t.Error("/api/internal/secret should be excluded even though /api/ is included")
	}
}

func TestRecordingFilter_ShouldRecord_IncludeHosts(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{IncludeHosts: []string{"example.com"}}

	allowed, _ := http.NewRequest("GET", "http://example.com/path", nil)
	denied, _ := http.NewRequest("GET", "http://other.com/path", nil)

	if !f.ShouldRecord(allowed) {
		t.Error("example.com should be recorded")
	}
	if f.ShouldRecord(denied) {
		t.Error("other.com should not be recorded")
	}
}

// ---------------------------------------------------------------------------
// RecordingFilter.RedactHeaders
// ---------------------------------------------------------------------------

func TestRecordingFilter_RedactHeaders_RemovesSensitive(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{ExcludeHeaders: []string{"Authorization", "Cookie"}}

	in := map[string]string{
		"Authorization": "Bearer secret",
		"Cookie":        "session=abc",
		"Content-Type":  "application/json",
	}

	out := f.RedactHeaders(in)

	if out["Authorization"] != "[REDACTED]" {
		t.Errorf("Authorization: got %q want [REDACTED]", out["Authorization"])
	}
	if out["Cookie"] != "[REDACTED]" {
		t.Errorf("Cookie: got %q want [REDACTED]", out["Cookie"])
	}
	if out["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: got %q want application/json", out["Content-Type"])
	}
}

func TestRecordingFilter_RedactHeaders_DoesNotMutateOriginal(t *testing.T) {
	t.Parallel()
	f := &RecordingFilter{ExcludeHeaders: []string{"Authorization"}}

	original := map[string]string{"Authorization": "Bearer secret"}
	_ = f.RedactHeaders(original)

	if original["Authorization"] != "Bearer secret" {
		t.Error("RedactHeaders must not mutate the input map")
	}
}

func TestRecordingFilter_RedactHeaders_NilFilter(t *testing.T) {
	t.Parallel()
	var f *RecordingFilter
	in := map[string]string{"Authorization": "Bearer secret"}
	out := f.RedactHeaders(in)
	if out["Authorization"] != "Bearer secret" {
		t.Errorf("nil filter should not redact anything, got %q", out["Authorization"])
	}
}
