package builtins

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrencyLimit verifies that the second concurrent request is rejected
// when MaxConcurrent=1.
func TestConcurrencyLimit(t *testing.T) {
	p := &TrafficShaping{cfg: TrafficShapingConfig{MaxConcurrent: 1}}
	ctx := context.Background()
	req1 := makeReq("GET", "http://example.com/api", "")
	req2 := makeReq("GET", "http://example.com/api", "")

	// First request should go through.
	result1, err := p.OnRequest(ctx, req1)
	if err != nil {
		t.Fatalf("unexpected error on first request: %v", err)
	}
	if result1 != nil && result1.MockedResponse != nil {
		t.Fatal("first request should not be rejected")
	}

	// Second concurrent request should be rejected.
	result2, err := p.OnRequest(ctx, req2)
	if err != nil {
		t.Fatalf("unexpected error on second request: %v", err)
	}
	if result2 == nil || result2.MockedResponse == nil {
		t.Fatal("second request should have a MockedResponse set")
	}
	if result2.MockedResponse.StatusCode != 503 {
		t.Errorf("expected status 503, got %d", result2.MockedResponse.StatusCode)
	}
	body, _ := io.ReadAll(result2.MockedResponse.Body)
	if string(body) != "too many concurrent requests" {
		t.Errorf("unexpected rejection body: %q", string(body))
	}
}

// TestInflightDecrementedAfterResponse verifies that after OnResponse the
// in-flight counter drops and the next request is allowed through.
func TestInflightDecrementedAfterResponse(t *testing.T) {
	p := &TrafficShaping{cfg: TrafficShapingConfig{MaxConcurrent: 1}}
	ctx := context.Background()
	req := makeReq("GET", "http://example.com/api", "")

	// Consume the slot.
	p.OnRequest(ctx, req) //nolint:errcheck

	// Release the slot via OnResponse.
	p.OnResponse(ctx, req, makeResp(200, "")) //nolint:errcheck

	// Next request should now go through.
	req2 := makeReq("GET", "http://example.com/api", "")
	result, err := p.OnRequest(ctx, req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("request after response should not be rejected")
	}
}

// TestCustomRejectStatus verifies that the RejectWith status code is used.
func TestCustomRejectStatus(t *testing.T) {
	p := &TrafficShaping{cfg: TrafficShapingConfig{MaxConcurrent: 1, RejectWith: 429}}
	ctx := context.Background()

	p.OnRequest(ctx, makeReq("GET", "http://example.com/", "")) //nolint:errcheck
	result, _ := p.OnRequest(ctx, makeReq("GET", "http://example.com/", ""))
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected rejection")
	}
	if result.MockedResponse.StatusCode != 429 {
		t.Errorf("expected 429, got %d", result.MockedResponse.StatusCode)
	}
}

// TestBandwidthThrottling verifies that reading N bytes takes at least N/BPS seconds.
func TestBandwidthThrottling(t *testing.T) {
	const bps = 1000          // 1 KB/s
	const dataSize = 500      // 500 bytes → should take ~0.5 s
	data := strings.Repeat("x", dataSize)

	p := &TrafficShaping{cfg: TrafficShapingConfig{BandwidthBPS: bps}}
	ctx := context.Background()
	req := makeReq("GET", "http://example.com/download", "")
	resp := makeResp(200, data)

	modResp, err := p.OnResponse(ctx, req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if modResp == nil {
		t.Fatal("expected a modified response with throttled body")
	}

	start := time.Now()
	buf, err := io.ReadAll(modResp.Body)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("error reading throttled body: %v", err)
	}
	if string(buf) != data {
		t.Error("throttled body content mismatch")
	}

	expected := time.Duration(float64(dataSize) / float64(bps) * float64(time.Second))
	if elapsed < expected {
		t.Errorf("read too fast: elapsed %v, expected at least %v", elapsed, expected)
	}
}

// TestMatchPath verifies that requests not matching MatchPath are not affected.
func TestMatchPath(t *testing.T) {
	p := &TrafficShaping{cfg: TrafficShapingConfig{MaxConcurrent: 1, MatchPath: "/api"}}
	ctx := context.Background()

	// Fill the slot for /api.
	p.OnRequest(ctx, makeReq("GET", "http://example.com/api", "")) //nolint:errcheck

	// A request to a different path should not be rejected.
	result, err := p.OnRequest(ctx, makeReq("GET", "http://example.com/static/file.js", ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("non-matching path should not be rejected")
	}
}

// TestConcurrentSafety exercises the plugin under parallel load to detect races.
func TestConcurrentSafety(t *testing.T) {
	p := &TrafficShaping{cfg: TrafficShapingConfig{MaxConcurrent: 5}}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := makeReq("GET", "http://example.com/stress", "")
			result, _ := p.OnRequest(ctx, req)
			if result == nil || result.MockedResponse == nil {
				// Request went through — simulate work then release.
				time.Sleep(5 * time.Millisecond)
				p.OnResponse(ctx, req, makeResp(200, "")) //nolint:errcheck
			}
		}()
	}
	wg.Wait()
}
