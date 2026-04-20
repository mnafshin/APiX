package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mnafshin/apix/internal/config"
)

func TestProxyRateLimiterAcquireRelease(t *testing.T) {
	t.Parallel()
	limiter := &proxyRateLimiter{
		maxConcurrent: 1,
		active:        make(map[string]int),
	}
	ip := "10.0.0.1"
	if !limiter.acquire(ip) {
		t.Fatal("expected first acquire to succeed")
	}
	if limiter.acquire(ip) {
		t.Fatal("expected second acquire to fail at max concurrent limit")
	}
	limiter.release(ip)
	if !limiter.acquire(ip) {
		t.Fatal("expected acquire to succeed after release")
	}
}

func TestHTTPProxyServeHTTPRateLimit(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := config.LoadConfig("/nonexistent/path/config.yaml")
	cfg.ProxyRateLimitPerSec = 1
	cfg.ProxyMaxConcurrentConnections = 50
	p := NewHTTPProxy("", nil, nil, TransportOptions{}, cfg)

	req1 := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rr1 := httptest.NewRecorder()
	p.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", rr1.Code, http.StatusOK)
	}

	req2 := httptest.NewRequest(http.MethodGet, upstream.URL, nil)
	req2.RemoteAddr = "10.0.0.1:5678"
	rr2 := httptest.NewRecorder()
	p.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", rr2.Code, http.StatusTooManyRequests)
	}
}
