package replay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/storage"
)

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestReplayRawRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("pong"))
	}))
	defer srv.Close()

	db := openTestDB(t)
	engine := NewEngine(db, nil)

	u, _ := url.Parse(srv.URL + "/ping")
	rawReq, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := engine.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:      rawReq,
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("body: got %q want %q", string(body), "pong")
	}
}

func TestReplayWithHeaderOverrides(t *testing.T) {
	t.Parallel()
	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Override")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	engine := NewEngine(db, nil)

	u, _ := url.Parse(srv.URL)
	rawReq, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := engine.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:      rawReq,
		OverrideHeaders: map[string]string{"X-Override": "injected"},
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedHeader != "injected" {
		t.Errorf("X-Override: got %q want %q", receivedHeader, "injected")
	}
}

func TestReplayWithBodyOverride(t *testing.T) {
	t.Parallel()
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	engine := NewEngine(db, nil)

	u, _ := url.Parse(srv.URL)
	rawReq, _ := http.NewRequest("POST", u.String(), strings.NewReader("original"))

	resp, err := engine.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:      rawReq,
		OverrideBody:    []byte("overridden"),
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedBody != "overridden" {
		t.Errorf("body: got %q want %q", receivedBody, "overridden")
	}
}

func TestReplayNoFollowRedirects(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final"))
	}))
	defer srv.Close()

	db := openTestDB(t)
	engine := NewEngine(db, nil)

	u, _ := url.Parse(srv.URL + "/redirect")
	rawReq, _ := http.NewRequest("GET", u.String(), nil)

	resp, err := engine.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:      rawReq,
		FollowRedirects: false,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Errorf("StatusCode: got %d want 301 (redirect not followed)", resp.StatusCode)
	}
}

// TestReplayFollowsRedirects verifies that with FollowRedirects=true the
// engine follows 301/302 chains and returns the final response.
func TestReplayFollowsRedirects(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/middle", http.StatusFound)
		case "/middle":
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("arrived"))
		}
	}))
	defer srv.Close()

	db := openTestDB(t)
	eng := NewEngine(db, nil)

	rawReq, _ := http.NewRequest("GET", srv.URL+"/start", nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RawRequest:      rawReq,
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d want 200 (after following redirects)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "arrived" {
		t.Errorf("body: got %q want %q", string(body), "arrived")
	}
}

// TestReplayUpstreamUnreachable verifies that a dial failure returns an error
// immediately instead of hanging.
func TestReplayUpstreamUnreachable(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	eng := NewEngine(db, nil)

	// Port 1 is never bound; the dial fails immediately.
	rawReq, _ := http.NewRequest("GET", "http://127.0.0.1:1/test", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := eng.ReplayRequest(ctx, &ReplayRequest{
		RawRequest:      rawReq,
		FollowRedirects: false,
	})
	if err == nil {
		t.Fatal("expected error for unreachable upstream, got nil")
	}
}

// TestReplayMethodOverride verifies that a stored GET can be replayed as a POST.
func TestReplayMethodOverride(t *testing.T) {
	t.Parallel()
	var receivedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)

	// Seed a stored GET request.
	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "test-get-replay",
		Method: "GET",
		URL:    srv.URL + "/resource",
	}); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}

	eng := NewEngine(db, nil)
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{
		RequestID:      "test-get-replay",
		OverrideMethod: "POST",
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedMethod != "POST" {
		t.Errorf("method: got %q want POST", receivedMethod)
	}
}
