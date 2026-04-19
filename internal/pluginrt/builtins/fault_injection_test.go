package builtins

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// ---- FaultInjection tests ----

func TestFaultInjection_AbortFault(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 100, FaultType: "abort", AbortStatus: http.StatusTeapot},
		},
	}
	req := makeReq("GET", "https://example.com/api", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if result.MockedResponse == nil {
		t.Fatal("expected MockedResponse to be set for abort fault")
	}
	if result.MockedResponse.StatusCode != http.StatusTeapot {
		t.Errorf("StatusCode: got %d want %d", result.MockedResponse.StatusCode, http.StatusTeapot)
	}
	body, _ := io.ReadAll(result.MockedResponse.Body)
	if string(body) != "fault injected" {
		t.Errorf("body: got %q want %q", string(body), "fault injected")
	}
}

func TestFaultInjection_AbortDefaultStatus(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 100, FaultType: "abort"},
		},
	}
	req := makeReq("GET", "https://example.com/", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result.MockedResponse.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("default abort status: got %d want 503", result.MockedResponse.StatusCode)
	}
}

func TestFaultInjection_DelayFault(t *testing.T) {
	t.Parallel()
	delay := 50 * time.Millisecond
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 100, FaultType: "delay", DelayDuration: delay},
		},
	}
	req := makeReq("GET", "https://example.com/slow", "")
	start := time.Now()
	result, err := p.OnRequest(context.Background(), req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request (delay applied), got nil")
	}
	if elapsed < delay {
		t.Errorf("delay fault: elapsed %v < expected %v", elapsed, delay)
	}
}

func TestFaultInjection_HeaderFault(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 100, FaultType: "header", HeaderName: "X-Chaos", HeaderValue: "injected"},
		},
	}
	req := makeReq("GET", "https://example.com/", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified request, got nil")
	}
	if result.Headers.Get("X-Chaos") != "injected" {
		t.Errorf("X-Chaos: got %q want %q", result.Headers.Get("X-Chaos"), "injected")
	}
}

func TestFaultInjection_PercentageZeroNeverTriggers(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 0, FaultType: "abort", AbortStatus: 503},
		},
	}
	for i := 0; i < 200; i++ {
		req := makeReq("GET", "https://example.com/", "")
		result, err := p.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("OnRequest: %v", err)
		}
		if result != nil {
			t.Fatalf("Percentage=0: expected nil on iteration %d, got modified request", i)
		}
	}
}

func TestFaultInjection_Percentage100AlwaysTriggers(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{Percentage: 100, FaultType: "abort", AbortStatus: 503},
		},
	}
	for i := 0; i < 50; i++ {
		req := makeReq("GET", "https://example.com/", "")
		result, err := p.OnRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("OnRequest: %v", err)
		}
		if result == nil || result.MockedResponse == nil {
			t.Fatalf("Percentage=100: expected abort on iteration %d, got nil", i)
		}
	}
}

func TestFaultInjection_PathMatch(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{MatchPath: "/api/", Percentage: 100, FaultType: "abort", AbortStatus: 503},
		},
	}

	// Should match.
	reqMatch := makeReq("GET", "https://example.com/api/users", "")
	result, err := p.OnRequest(context.Background(), reqMatch)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Error("expected abort for /api/users")
	}

	// Should not match.
	reqNoMatch := makeReq("GET", "https://example.com/health", "")
	result2, err := p.OnRequest(context.Background(), reqNoMatch)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result2 != nil {
		t.Error("expected nil (no match) for /health")
	}
}

func TestFaultInjection_MethodMatch(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{
			{MatchMethod: "POST", Percentage: 100, FaultType: "abort", AbortStatus: 503},
		},
	}

	// POST should match.
	reqPost := makeReq("POST", "https://example.com/data", "")
	result, err := p.OnRequest(context.Background(), reqPost)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result == nil || result.MockedResponse == nil {
		t.Error("expected abort for POST")
	}

	// GET should not match.
	reqGet := makeReq("GET", "https://example.com/data", "")
	result2, err := p.OnRequest(context.Background(), reqGet)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result2 != nil {
		t.Error("expected nil (no match) for GET")
	}
}

func TestFaultInjection_NoRulesPassThrough(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{}
	req := makeReq("GET", "https://example.com/", "")
	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("expected nil pass-through with no rules")
	}
}

func TestFaultInjection_OnResponsePassThrough(t *testing.T) {
	t.Parallel()
	p := &FaultInjection{
		Rules: []FaultRule{{Percentage: 100, FaultType: "abort"}},
	}
	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "ok")
	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result != nil {
		t.Error("expected nil from OnResponse (pass-through)")
	}
}
