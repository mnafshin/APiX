package replay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReplayTLSVerification ensures TLS verification is enforced by default
// and can be disabled via ClientConfig.SkipTLSVerify.
func TestReplayTLSVerification(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secure"))
	}))
	defer srv.Close()

	db := openTestDB(t)

	// Default engine verifies TLS — should error for self-signed cert
	eng := NewEngine(db, nil)
	rawReq, _ := http.NewRequest("GET", srv.URL, nil)
	_, err := eng.ReplayRequest(context.Background(), &ReplayRequest{RawRequest: rawReq})
	if err == nil {
		t.Fatal("expected TLS verification error with system CAs and self-signed cert")
	}

	// Insecure engine: SkipTLSVerify => should succeed
	engInsecure := NewEngine(db, &ClientConfig{SkipTLSVerify: true})
	resp, err := engInsecure.ReplayRequest(context.Background(), &ReplayRequest{RawRequest: rawReq})
	if err != nil {
		t.Fatalf("expected no error with SkipTLSVerify, got %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "secure" {
		t.Fatalf("unexpected body: %q", string(b))
	}
}

// TestReplayLargeBody verifies that replay correctly sends large request bodies
// to upstreams (avoids accidental truncation).
func TestReplayLargeBody(t *testing.T) {
	t.Parallel()
	const size = 1 << 20 // 1MiB
	payload := bytes.Repeat([]byte("a"), size)
	var receivedLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedLen = len(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	db := openTestDB(t)
	eng := NewEngine(db, nil)
	rawReq, _ := http.NewRequest("POST", srv.URL, bytes.NewReader(payload))
	resp, err := eng.ReplayRequest(context.Background(), &ReplayRequest{RawRequest: rawReq})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	resp.Body.Close()

	if receivedLen != size {
		t.Fatalf("received len: got %d want %d", receivedLen, size)
	}
}

// TestReplayRedirectLoop ensures the engine surfaces an error when following
// a redirect loop instead of hanging indefinitely.
func TestReplayRedirectLoop(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	db := openTestDB(t)
	eng := NewEngine(db, nil)
	rawReq, _ := http.NewRequest("GET", srv.URL+"/loop", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := eng.ReplayRequest(ctx, &ReplayRequest{RawRequest: rawReq, FollowRedirects: true})
	if err == nil {
		t.Fatal("expected error due to too many redirects")
	}
}
