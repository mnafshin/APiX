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
