package builtins

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// TestCachingHitReturnsCachedResponse verifies that a cached entry is served
// on subsequent requests without going upstream.
func TestCachingHitReturnsCachedResponse(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{Capacity: 10, TTL: time.Minute}}

	req := makeReq("GET", "https://example.com/data", "")
	resp := makeResp(200, `{"cached":true}`)
	resp.Headers.Set("Content-Type", "application/json")

	// Populate the cache via OnResponse.
	modified, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if modified == nil {
		t.Fatal("OnResponse should return a modified response (re-wrapped body)")
	}

	// Second request — should hit the cache.
	req2 := makeReq("GET", "https://example.com/data", "")
	result, err := p.OnRequest(context.Background(), req2)
	if err != nil {
		t.Fatalf("OnRequest (cache hit): %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected MockedResponse to be set on cache hit")
	}
	if result.MockedResponse.StatusCode != 200 {
		t.Errorf("StatusCode: got %d want 200", result.MockedResponse.StatusCode)
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != `{"cached":true}` {
		t.Errorf("body: got %q", string(body))
	}
	if result.MockedResponse.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q", result.MockedResponse.Headers.Get("Content-Type"))
	}
}

// TestCachingMissPassesThrough verifies that an uncached request returns nil
// (allowing the proxy to forward upstream).
func TestCachingMissPassesThrough(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{Capacity: 10, TTL: time.Minute}}

	req := makeReq("GET", "https://example.com/uncached", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("expected nil (cache miss), got non-nil result")
	}
}

// TestCachingTTLExpiry verifies that an entry is evicted after its TTL elapses.
func TestCachingTTLExpiry(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{Capacity: 10, TTL: 30 * time.Millisecond}}

	req := makeReq("GET", "https://example.com/short-lived", "")
	resp := makeResp(200, "body")

	_, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}

	// Should hit before TTL.
	result, err := p.OnRequest(context.Background(), makeReq("GET", "https://example.com/short-lived", ""))
	if err != nil {
		t.Fatalf("OnRequest before expiry: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected cache hit before TTL")
	}

	// Wait for TTL to elapse.
	time.Sleep(50 * time.Millisecond)

	// Should miss after TTL.
	result, err = p.OnRequest(context.Background(), makeReq("GET", "https://example.com/short-lived", ""))
	if err != nil {
		t.Fatalf("OnRequest after expiry: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Error("expected cache miss after TTL expiry")
	}
}

// TestCachingOnlyConfiguredMethodsCached verifies that only allowed HTTP methods
// are cached, and others pass through uncached.
func TestCachingOnlyConfiguredMethodsCached(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{
		Capacity:     10,
		TTL:          time.Minute,
		CacheMethods: []string{"GET"},
	}}

	// POST should not be cached.
	postReq := makeReq("POST", "https://example.com/data", "body")
	postResp := makeResp(200, "post-response")

	modified, err := p.OnResponse(context.Background(), postReq, postResp)
	if err != nil {
		t.Fatalf("OnResponse POST: %v", err)
	}
	// OnResponse returns nil for non-cached methods.
	if modified != nil {
		t.Error("OnResponse should return nil for non-cached method POST")
	}

	// Subsequent POST request should not hit the cache.
	result, err := p.OnRequest(context.Background(), makeReq("POST", "https://example.com/data", "body"))
	if err != nil {
		t.Fatalf("OnRequest POST: %v", err)
	}
	if result != nil {
		t.Error("POST request should never return a cached response")
	}

	// GET should be cached.
	getReq := makeReq("GET", "https://example.com/data", "")
	_, err = p.OnResponse(context.Background(), getReq, makeResp(200, "get-response"))
	if err != nil {
		t.Fatalf("OnResponse GET: %v", err)
	}

	result, err = p.OnRequest(context.Background(), makeReq("GET", "https://example.com/data", ""))
	if err != nil {
		t.Fatalf("OnRequest GET: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("GET request should return a cached response")
	}
}

// TestCachingCapacityEvictsLRU verifies that when the cache is full the least
// recently used entry is evicted to make room.
func TestCachingCapacityEvictsLRU(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{Capacity: 2, TTL: time.Minute}}

	urlA := "https://example.com/a"
	urlB := "https://example.com/b"
	urlC := "https://example.com/c"

	store := func(u string) {
		t.Helper()
		_, err := p.OnResponse(context.Background(), makeReq("GET", u, ""), makeResp(200, u))
		if err != nil {
			t.Fatalf("OnResponse %s: %v", u, err)
		}
	}
	hit := func(u string) bool {
		t.Helper()
		res, err := p.OnRequest(context.Background(), makeReq("GET", u, ""))
		if err != nil {
			t.Fatalf("OnRequest %s: %v", u, err)
		}
		return res != nil && res.MockedResponse != nil
	}

	store(urlA) // LRU: [A]
	store(urlB) // LRU: [B, A]

	// Access A to make it MRU.
	if !hit(urlA) {
		t.Fatal("expected cache hit for A")
	}
	// LRU: [A, B]

	// Adding C should evict B (LRU).
	store(urlC) // LRU: [C, A]

	if hit(urlB) {
		t.Error("B should have been evicted (LRU)")
	}
	if !hit(urlA) {
		t.Error("A should still be cached (was MRU)")
	}
	if !hit(urlC) {
		t.Error("C should be cached (just added)")
	}
}

// TestCachingNon2xxNotCached verifies that non-2xx responses are not stored.
func TestCachingNon2xxNotCached(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{cfg: CachingConfig{Capacity: 10, TTL: time.Minute}}

	for _, status := range []int{301, 400, 404, 500, 503} {
		status := status
		t.Run(plugins.ProxyResponse{StatusCode: status}.Status, func(t *testing.T) {
			t.Parallel()
			req := makeReq("GET", "https://example.com/error", "")
			resp := makeResp(status, "error-body")

			modified, err := p.OnResponse(context.Background(), req, resp)
			if err != nil {
				t.Fatalf("OnResponse: %v", err)
			}
			if modified != nil {
				t.Errorf("status %d should not be cached", status)
			}

			result, err := p.OnRequest(context.Background(), makeReq("GET", "https://example.com/error", ""))
			if err != nil {
				t.Fatalf("OnRequest: %v", err)
			}
			if result != nil && result.MockedResponse != nil {
				t.Errorf("status %d response should not appear in cache", status)
			}
		})
	}
}

// TestCachingDefaultConfig ensures zero-value CachingConfig uses sensible defaults.
func TestCachingDefaultConfig(t *testing.T) {
	t.Parallel()
	p := &CachingPlugin{} // all zero — defaults apply

	req := makeReq("GET", "https://example.com/default", "")
	resp := makeResp(200, "default-body")

	_, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}

	result, err := p.OnRequest(context.Background(), makeReq("GET", "https://example.com/default", ""))
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected cache hit with default config")
	}
}
