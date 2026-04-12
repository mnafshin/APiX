package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/logging"
)

func TestHTTPProxySlowlogWarns(t *testing.T) {
	// Capture logs to a buffer
	var buf bytes.Buffer
	logging.Init(&buf)
	defer logging.Init(nil)

	// Upstream server that sleeps to simulate a slow upstream
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := &config.Config{SlowlogThresholdMs: 10, MaxBodySizeMB: 1}
	p := NewHTTPProxy("127.0.0.1:0", nil, nil, TransportOptions{}, cfg)

	req := httptest.NewRequest(http.MethodGet, srv.URL, nil)
	rr := httptest.NewRecorder()

	p.handleHTTP(rr, req)

	out := buf.String()
	if !strings.Contains(out, "slow request") {
		t.Fatalf("expected slow request warning in logs, got: %s", out)
	}
}
