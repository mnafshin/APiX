package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mnafshin/apix/internal/config"
)

func TestHTTPProxyAccessLogWritesJSON(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	logPath := filepath.Join(t.TempDir(), "access.log")
	cfg := &config.Config{
		MaxBodySizeMB:      1,
		AccessLogEnabled:   true,
		AccessLogFormat:    "json",
		AccessLogPath:      logPath,
		SlowlogThresholdMs: 0,
	}
	p := NewHTTPProxy("127.0.0.1:0", nil, nil, TransportOptions{}, cfg)

	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	rr := httptest.NewRecorder()
	p.handleHTTP(rr, req)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"method":"GET"`) {
		t.Fatalf("expected method in access log, got: %s", out)
	}
	if !strings.Contains(out, `"status":200`) {
		t.Fatalf("expected status in access log, got: %s", out)
	}
	if !strings.Contains(out, `"request_id":"`) {
		t.Fatalf("expected request_id in access log, got: %s", out)
	}
}
