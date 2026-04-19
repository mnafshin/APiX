package builtins

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// TrafficShapingConfig holds the configuration for the TrafficShaping plugin.
type TrafficShapingConfig struct {
	// MaxConcurrent is the maximum number of in-flight requests allowed concurrently.
	// Zero means unlimited.
	MaxConcurrent int
	// BandwidthBPS limits response body read throughput in bytes per second.
	// Zero means unlimited.
	BandwidthBPS int64
	// MatchPath is a substring filter on the request URL path. Empty matches all.
	MatchPath string
	// RejectWith is the HTTP status code returned when concurrency is exceeded.
	// Defaults to 503 when zero.
	RejectWith int
}

// TrafficShaping is a plugin that enforces concurrency limits and bandwidth
// throttling on proxied requests.
type TrafficShaping struct {
	cfg      TrafficShapingConfig
	mu       sync.Mutex
	inflight int
}

func (p *TrafficShaping) Name() string    { return "traffic-shaping" }
func (p *TrafficShaping) Version() string { return "1.0.0" }
func (p *TrafficShaping) Description() string {
	return "Enforces concurrency limits and per-response bandwidth throttling."
}

func (p *TrafficShaping) rejectStatus() int {
	if p.cfg.RejectWith == 0 {
		return http.StatusServiceUnavailable
	}
	return p.cfg.RejectWith
}

func (p *TrafficShaping) matchesPath(req *plugins.ProxyRequest) bool {
	if p.cfg.MatchPath == "" {
		return true
	}
	return strings.Contains(req.URL.Path, p.cfg.MatchPath)
}

// OnRequest enforces the concurrency limit. If the limit is exceeded, a
// synthetic rejection response is set on the request without hitting upstream.
func (p *TrafficShaping) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if !p.matchesPath(req) {
		return nil, nil
	}

	if p.cfg.MaxConcurrent > 0 {
		p.mu.Lock()
		if p.inflight >= p.cfg.MaxConcurrent {
			p.mu.Unlock()
			status := p.rejectStatus()
			hdrs := make(http.Header)
			hdrs.Set("Content-Type", "text/plain")
			clone := req.Clone(req.Body)
			clone.MockedResponse = &plugins.ProxyResponse{
				StatusCode: status,
				Status:     http.StatusText(status),
				Headers:    hdrs,
				Body:       io.NopCloser(bytes.NewBufferString("too many concurrent requests")),
			}
			return clone, nil
		}
		p.inflight++
		p.mu.Unlock()
	}

	return nil, nil
}

// OnResponse decrements the in-flight counter and optionally wraps the response
// body with a throttled reader to enforce bandwidth limits.
func (p *TrafficShaping) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if !p.matchesPath(req) {
		return nil, nil
	}

	if p.cfg.MaxConcurrent > 0 {
		p.mu.Lock()
		if p.inflight > 0 {
			p.inflight--
		}
		p.mu.Unlock()
	}

	if p.cfg.BandwidthBPS > 0 {
		throttled := resp.Clone(io.NopCloser(&throttledReader{
			r:           resp.Body,
			bytesPerSec: p.cfg.BandwidthBPS,
		}))
		return throttled, nil
	}

	return nil, nil
}

// throttledReader wraps an io.Reader and enforces a bytes-per-second limit by
// sleeping after each read to maintain the configured throughput rate.
type throttledReader struct {
	r           io.Reader
	bytesPerSec int64
	lastRead    time.Time
}

func (t *throttledReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 && t.bytesPerSec > 0 {
		// Calculate how long this chunk should have taken at the target rate.
		expected := time.Duration(float64(n) / float64(t.bytesPerSec) * float64(time.Second))
		if !t.lastRead.IsZero() {
			elapsed := time.Since(t.lastRead)
			if elapsed < expected {
				time.Sleep(expected - elapsed)
			}
		} else {
			time.Sleep(expected)
		}
		t.lastRead = time.Now()
	}
	return n, err
}
