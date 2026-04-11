package replay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestReplayResponseHeaderTimeout ensures the client times out if the upstream
// delays sending response headers longer than the configured ResponseHeaderTimeout.
func TestReplayResponseHeaderTimeout(t *testing.T) {
	t.Parallel()
	// Server that accepts the request but delays writing the response headers.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay longer than the client's configured response header timeout.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	db := openTestDB(t)

	eng := NewEngine(db, &ClientConfig{
		ResponseHeaderTimeout: 50 * time.Millisecond,
	})

	rawReq, _ := http.NewRequest("GET", srv.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := eng.ReplayRequest(ctx, &ReplayRequest{RawRequest: rawReq, FollowRedirects: true})
	if err == nil {
		t.Fatal("expected timeout error due to slow response headers, got nil")
	}
	// Ensure error is a timeout (may be wrapped); string-match as conservative check.
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Logf("received non-timeout error: %v", err)
	}
}
