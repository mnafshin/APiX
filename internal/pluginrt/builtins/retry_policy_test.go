package builtins

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryPolicy_NilRaw(t *testing.T) {
	p := &RetryPolicy{RetryPolicyConfig{Rules: []RetryRule{
		{RetryOnStatus: []int{503}, MaxAttempts: 3},
	}}}
	// makeReq from builtins_test.go does not set Raw, so Raw is nil by default.
	req := makeReq(http.MethodGet, "http://example.com/api", "")
	resp := makeResp(503, "")

	got, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil response when Raw is nil, got %+v", got)
	}
}

func TestRetryPolicy_POSTNotRetried(t *testing.T) {
	p := &RetryPolicy{RetryPolicyConfig{Rules: []RetryRule{
		{RetryOnStatus: []int{503}, MaxAttempts: 3},
		// default MatchMethods: GET, HEAD, OPTIONS — POST not included
	}}}
	req := makeReq(http.MethodPost, "http://example.com/api", "")
	rawReq, _ := http.NewRequest(http.MethodPost, "http://example.com/api", nil)
	req.Raw = rawReq
	resp := makeResp(503, "")

	got, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (no retry for POST), got %+v", got)
	}
}

func TestRetryPolicy_MaxAttemptsZero(t *testing.T) {
	p := &RetryPolicy{RetryPolicyConfig{Rules: []RetryRule{
		{RetryOnStatus: []int{503}, MaxAttempts: 0},
	}}}
	req := makeReq(http.MethodGet, "http://example.com/api", "")
	rawReq, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Raw = rawReq
	resp := makeResp(503, "")

	got, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when MaxAttempts=0, got %+v", got)
	}
}

func TestRetryPolicy_ExponentialBackoffDuration(t *testing.T) {
	rule := &RetryRule{
		Backoff:     "exponential",
		BackoffBase: 100 * time.Millisecond,
		BackoffMax:  5 * time.Second,
	}
	cases := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{10, 5 * time.Second}, // capped at BackoffMax
	}
	for _, c := range cases {
		got := rule.sleepDuration(c.attempt)
		if got != c.expected {
			t.Errorf("attempt %d: expected %v, got %v", c.attempt, c.expected, got)
		}
	}
}

func TestRetryPolicy_FixedBackoffDuration(t *testing.T) {
	rule := &RetryRule{
		Backoff:     "fixed",
		BackoffBase: 200 * time.Millisecond,
		BackoffMax:  5 * time.Second,
	}
	for _, attempt := range []int{0, 1, 2, 5} {
		got := rule.sleepDuration(attempt)
		if got != 200*time.Millisecond {
			t.Errorf("attempt %d: expected 200ms, got %v", attempt, got)
		}
	}
}

func TestRetryPolicy_NoMatchingRule(t *testing.T) {
	p := &RetryPolicy{RetryPolicyConfig{Rules: []RetryRule{
		{RetryOnStatus: []int{502}, MaxAttempts: 3},
	}}}
	req := makeReq(http.MethodGet, "http://example.com/api", "")
	rawReq, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Raw = rawReq
	resp := makeResp(503, "") // 503 not in RetryOnStatus

	got, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when status does not match, got %+v", got)
	}
}

func TestRetryPolicy_PathFilter(t *testing.T) {
	p := &RetryPolicy{RetryPolicyConfig{Rules: []RetryRule{
		{MatchPath: "/special", RetryOnStatus: []int{503}, MaxAttempts: 3},
	}}}
	req := makeReq(http.MethodGet, "http://example.com/other", "")
	rawReq, _ := http.NewRequest(http.MethodGet, "http://example.com/other", nil)
	req.Raw = rawReq
	resp := makeResp(503, "")

	got, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when path doesn't match, got %+v", got)
	}
}

func TestRetryPolicy_OnRequestNoOp(t *testing.T) {
	p := &RetryPolicy{}
	req := makeReq(http.MethodGet, "http://example.com/", "")

	got, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("OnRequest should be a no-op, got %+v", got)
	}
}

func TestRetryPolicy_Metadata(t *testing.T) {
	p := &RetryPolicy{}
	if p.Name() != "retry-policy" {
		t.Errorf("unexpected Name: %s", p.Name())
	}
	if p.Version() == "" {
		t.Error("Version should not be empty")
	}
	if p.Description() == "" {
		t.Error("Description should not be empty")
	}
}
