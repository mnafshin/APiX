package builtins

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// simulateFailures sends n synthetic 5xx responses through the breaker.
func simulateFailures(t *testing.T, cb *CircuitBreaker, req *plugins.ProxyRequest, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		resp := makeResp(500, "error")
		_, err := cb.OnResponse(context.Background(), req, resp)
		if err != nil {
			t.Fatalf("OnResponse failure %d: %v", i+1, err)
		}
	}
}

// simulateSuccesses sends n synthetic 2xx responses through the breaker.
func simulateSuccesses(t *testing.T, cb *CircuitBreaker, req *plugins.ProxyRequest, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		resp := makeResp(200, "ok")
		_, err := cb.OnResponse(context.Background(), req, resp)
		if err != nil {
			t.Fatalf("OnResponse success %d: %v", i+1, err)
		}
	}
}

// TestCircuitBreakerOpensAfterFailureThreshold verifies CLOSED → OPEN transition.
func TestCircuitBreakerOpensAfterFailureThreshold(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{FailureThreshold: 3, SuccessThreshold: 2, OpenTimeout: 30 * time.Second}
	req := makeReq("GET", "https://api.example.com/data", "")

	// Under threshold — breaker should stay CLOSED.
	simulateFailures(t, cb, req, 2)
	result, err := cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("breaker should be CLOSED after 2 failures (threshold 3)")
	}

	// Hit threshold — breaker should OPEN.
	simulateFailures(t, cb, req, 1)
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("breaker should be OPEN after 3 failures")
	}
	if result.MockedResponse.StatusCode != 503 {
		t.Errorf("expected 503, got %d", result.MockedResponse.StatusCode)
	}
}

// TestCircuitBreakerReturns503WhenOpen verifies that OPEN state short-circuits with 503.
func TestCircuitBreakerReturns503WhenOpen(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{FailureThreshold: 1, SuccessThreshold: 2, OpenTimeout: 30 * time.Second}
	req := makeReq("GET", "https://svc.example.com/resource", "")

	simulateFailures(t, cb, req, 1) // open the breaker

	for i := 0; i < 5; i++ {
		result, err := cb.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("iteration %d OnRequest: %v", i, err)
		}
		if result == nil || result.MockedResponse == nil {
			t.Fatalf("iteration %d: expected 503 mocked response when OPEN", i)
		}
		if result.MockedResponse.StatusCode != 503 {
			t.Errorf("iteration %d: expected 503, got %d", i, result.MockedResponse.StatusCode)
		}
		body, _ := io.ReadAll(result.MockedResponse.Body)
		if len(body) == 0 {
			t.Errorf("iteration %d: expected non-empty body", i)
		}
	}
}

// TestCircuitBreakerTransitionsToHalfOpenAfterTimeout verifies OPEN → HALF_OPEN transition.
func TestCircuitBreakerTransitionsToHalfOpenAfterTimeout(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenTimeout:      50 * time.Millisecond, // short for testing
	}
	req := makeReq("GET", "https://probe.example.com/health", "")

	simulateFailures(t, cb, req, 1) // open the breaker

	// Before timeout — should return 503.
	result, err := cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest before timeout: %v", err)
	}
	if result == nil || result.MockedResponse == nil || result.MockedResponse.StatusCode != 503 {
		t.Fatal("expected 503 while still OPEN")
	}

	// Wait for timeout to elapse.
	time.Sleep(60 * time.Millisecond)

	// After timeout — breaker should move to HALF_OPEN and let the probe through.
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest after timeout: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("expected probe request to pass through in HALF_OPEN state")
	}
}

// TestCircuitBreakerClosesAfterSuccessThresholdInHalfOpen verifies HALF_OPEN → CLOSED.
func TestCircuitBreakerClosesAfterSuccessThresholdInHalfOpen(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenTimeout:      10 * time.Millisecond,
	}
	req := makeReq("GET", "https://recover.example.com/api", "")

	simulateFailures(t, cb, req, 1) // open the breaker
	time.Sleep(20 * time.Millisecond)

	// First probe allowed through (transitions to HALF_OPEN).
	result, err := cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("first probe OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("first probe should pass through")
	}

	// Simulate first successful response.
	simulateSuccesses(t, cb, req, 1)

	// Second probe — halfOpenPending was cleared, allow another probe.
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("second probe OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("second probe should pass through")
	}

	// Simulate second successful response — should close the breaker.
	simulateSuccesses(t, cb, req, 1)

	// Breaker should be CLOSED again — normal requests pass through.
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("post-close OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("breaker should be CLOSED and let requests through")
	}
}

// TestCircuitBreakerMatchHosts verifies that MatchHosts filters which hosts are protected.
func TestCircuitBreakerMatchHosts(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
		MatchHosts:       []string{"protected.example.com"},
	}

	protected := makeReq("GET", "https://protected.example.com/api", "")
	unprotected := makeReq("GET", "https://other.example.com/api", "")

	// Open the breaker for the protected host.
	simulateFailures(t, cb, protected, 1)

	// Protected host should get 503.
	result, err := cb.OnRequest(context.Background(), protected)
	if err != nil {
		t.Fatalf("protected OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil || result.MockedResponse.StatusCode != 503 {
		t.Fatal("protected host should receive 503 when circuit is open")
	}

	// Unprotected host should pass through normally.
	result, err = cb.OnRequest(context.Background(), unprotected)
	if err != nil {
		t.Fatalf("unprotected OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("unprotected host should not be affected by circuit breaker")
	}
}

// TestCircuitBreakerHalfOpenFailureReopens verifies that a failure in HALF_OPEN re-opens.
func TestCircuitBreakerHalfOpenFailureReopens(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenTimeout:      10 * time.Millisecond,
	}
	req := makeReq("GET", "https://flaky.example.com/api", "")

	simulateFailures(t, cb, req, 1) // open the breaker
	time.Sleep(20 * time.Millisecond)

	// Probe passes through (HALF_OPEN).
	result, err := cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("probe OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("probe should pass through in HALF_OPEN")
	}

	// Probe fails — should re-open.
	simulateFailures(t, cb, req, 1)

	// Next request should get 503 again.
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("post-reopen OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil || result.MockedResponse.StatusCode != 503 {
		t.Fatal("circuit should be re-opened after failed probe")
	}
}

// TestCircuitBreakerDefaultConfig verifies that zero-value config uses sensible defaults.
func TestCircuitBreakerDefaultConfig(t *testing.T) {
	t.Parallel()
	cb := &CircuitBreaker{} // all zero — defaults apply
	req := makeReq("GET", "https://default.example.com/", "")

	// Default threshold is 5, so 4 failures should not open.
	simulateFailures(t, cb, req, 4)
	result, err := cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil && result.MockedResponse != nil {
		t.Fatal("breaker should still be CLOSED after 4 failures with default threshold 5")
	}

	// 5th failure should open.
	simulateFailures(t, cb, req, 1)
	result, err = cb.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Fatal("breaker should be OPEN after 5 failures (default threshold)")
	}
}
