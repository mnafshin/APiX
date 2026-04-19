package builtins

import (
	"context"
	"testing"
	"time"
)

func TestLatencyModifier_FixedDelay(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{FixedDelay: 30 * time.Millisecond},
		},
	})

	req := makeReq("GET", "https://example.com/api", "")
	start := time.Now()
	result, err := p.OnRequest(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("expected nil (pass-through), got modified request")
	}
	if elapsed < 25*time.Millisecond {
		t.Errorf("expected >= 25ms delay, got %v", elapsed)
	}
}

func TestLatencyModifier_JitterAddsLatency(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{FixedDelay: 10 * time.Millisecond, JitterMax: 20 * time.Millisecond},
		},
	})

	req := makeReq("GET", "https://example.com/", "")
	start := time.Now()
	_, err := p.OnRequest(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	// Minimum is FixedDelay; allow a bit of scheduling slack.
	if elapsed < 8*time.Millisecond {
		t.Errorf("expected >= 10ms delay (fixed floor), got %v", elapsed)
	}
}

func TestLatencyModifier_AddHeaders(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{AddHeaders: map[string]string{"X-Injected": "yes", "X-Another": "value"}},
		},
	})

	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "body")

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified response, got nil")
	}
	if result.Headers.Get("X-Injected") != "yes" {
		t.Errorf("X-Injected: got %q", result.Headers.Get("X-Injected"))
	}
	if result.Headers.Get("X-Another") != "value" {
		t.Errorf("X-Another: got %q", result.Headers.Get("X-Another"))
	}
}

func TestLatencyModifier_RemoveHeaders(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{RemoveHeaders: []string{"X-Secret", "X-Internal"}},
		},
	})

	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "body")
	resp.Headers.Set("X-Secret", "sensitive")
	resp.Headers.Set("X-Internal", "debug-info")
	resp.Headers.Set("X-Keep", "stays")

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified response, got nil")
	}
	if v := result.Headers.Get("X-Secret"); v != "" {
		t.Errorf("X-Secret should be removed, got %q", v)
	}
	if v := result.Headers.Get("X-Internal"); v != "" {
		t.Errorf("X-Internal should be removed, got %q", v)
	}
	if v := result.Headers.Get("X-Keep"); v != "stays" {
		t.Errorf("X-Keep should be preserved, got %q", v)
	}
}

func TestLatencyModifier_StatusRemap(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{StatusRemap: map[int]int{200: 418, 404: 200}},
		},
	})

	req := makeReq("GET", "https://example.com/tea", "")
	resp := makeResp(200, "I'm a teapot")

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result == nil {
		t.Fatal("expected modified response, got nil")
	}
	if result.StatusCode != 418 {
		t.Errorf("StatusCode: got %d, want 418", result.StatusCode)
	}
}

func TestLatencyModifier_PathMatch_Skips(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{
				MatchPath:  "/admin",
				AddHeaders: map[string]string{"X-Restricted": "true"},
				StatusRemap: map[int]int{200: 403},
			},
		},
	})

	// Request to a non-matching path — rule must not apply.
	req := makeReq("GET", "https://example.com/public/page", "")
	resp := makeResp(200, "ok")

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	// nil means pass-through (no modification).
	if result != nil {
		t.Errorf("expected nil (no match), got modified response with status %d", result.StatusCode)
	}
}

func TestLatencyModifier_MethodMatch_Skips(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{
				MatchMethod: "POST",
				FixedDelay:  50 * time.Millisecond,
			},
		},
	})

	// GET request — rule only applies to POST; should be a no-op with no delay.
	req := makeReq("GET", "https://example.com/resource", "")
	start := time.Now()
	result, err := p.OnRequest(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Error("expected nil (pass-through)")
	}
	if elapsed >= 40*time.Millisecond {
		t.Errorf("non-matching method should not delay; got %v", elapsed)
	}
}

func TestLatencyModifier_NoDelay_NoModification(t *testing.T) {
	t.Parallel()
	p := NewLatencyModifier(LatencyModifierConfig{
		Rules: []LatencyRule{
			{AddHeaders: map[string]string{}},
		},
	})

	req := makeReq("GET", "https://example.com/", "")
	resp := makeResp(200, "body")

	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	// Empty AddHeaders → no modification → nil.
	if result != nil {
		t.Error("expected nil when no effective modification, got non-nil")
	}
}
