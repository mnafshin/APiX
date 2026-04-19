package builtins

import (
	"context"
	"io"
	"net/http"
	"testing"
)

// TestRateLimiterBurstAllowsThenRejects verifies that a burst=3, rate=0 bucket
// allows exactly 3 requests and rejects the 4th.
func TestRateLimiterBurstAllowsThenRejects(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimiterConfig{
		Rules: []RateRule{
			{KeySource: "global", Rate: 0, Burst: 3, RejectWith: 0},
		},
	})

	req := makeReq("GET", "https://example.com/api", "")

	for i := 1; i <= 3; i++ {
		result, err := rl.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if result != nil && result.MockedResponse != nil {
			t.Fatalf("request %d: expected pass-through, got rejection (status %d)", i, result.MockedResponse.StatusCode)
		}
	}

	// 4th request must be rejected.
	result, err := rl.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("4th request: unexpected error: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("4th request: expected rejection response, got nil")
	}
	if result.MockedResponse.StatusCode != http.StatusTooManyRequests {
		t.Errorf("4th request: expected 429, got %d", result.MockedResponse.StatusCode)
	}
}

// TestRateLimiterDifferentKeysAreIndependent verifies that distinct key values
// maintain independent buckets and do not interfere with one another.
func TestRateLimiterDifferentKeysAreIndependent(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimiterConfig{
		Rules: []RateRule{
			{KeySource: "header:X-User", Rate: 0, Burst: 1},
		},
	})

	// User A exhausts its own bucket.
	reqA := makeReq("GET", "https://example.com/", "")
	reqA.Headers.Set("X-User", "alice")

	result, err := rl.OnRequest(context.Background(), reqA)
	if err != nil {
		t.Fatalf("alice request 1: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("alice request 1: expected pass-through")
	}

	result, err = rl.OnRequest(context.Background(), reqA)
	if err != nil {
		t.Fatalf("alice request 2: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("alice request 2: expected rejection")
	}

	// User B still has a full independent bucket.
	reqB := makeReq("GET", "https://example.com/", "")
	reqB.Headers.Set("X-User", "bob")

	result, err = rl.OnRequest(context.Background(), reqB)
	if err != nil {
		t.Fatalf("bob request 1: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("bob request 1: expected pass-through (independent bucket)")
	}
}

// TestRateLimiterRejectionStatusAndRetryAfter verifies that a rejected request
// carries the configured status code and a Retry-After: 1 header.
func TestRateLimiterRejectionStatusAndRetryAfter(t *testing.T) {
	t.Parallel()
	customStatus := 503
	rl := NewRateLimiter(RateLimiterConfig{
		Rules: []RateRule{
			{KeySource: "global", Rate: 0, Burst: 0, RejectWith: customStatus},
		},
	})

	req := makeReq("GET", "https://example.com/resource", "")
	result, err := rl.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("expected rejection response")
	}

	got := result.MockedResponse.StatusCode
	if got != customStatus {
		t.Errorf("StatusCode: got %d want %d", got, customStatus)
	}

	retryAfter := result.MockedResponse.Headers.Get("Retry-After")
	if retryAfter != "1" {
		t.Errorf("Retry-After: got %q want %q", retryAfter, "1")
	}

	body, _ := io.ReadAll(result.MockedResponse.Body)
	if len(body) == 0 {
		t.Error("expected non-empty rejection body")
	}
}

// TestRateLimiterOnResponseIsNoop verifies that OnResponse always returns nil.
func TestRateLimiterOnResponseIsNoop(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimiterConfig{})
	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "ok")

	result, err := rl.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result != nil {
		t.Error("OnResponse: expected nil, got non-nil response")
	}
}
