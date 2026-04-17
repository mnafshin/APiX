package server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/peer"
)

func peerCtx(addr string) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		Addr: &fakeAddr{addr},
	})
}

type fakeAddr struct{ s string }

func (f *fakeAddr) Network() string { return "tcp" }
func (f *fakeAddr) String() string  { return f.s }

var _ net.Addr = (*fakeAddr)(nil)

func TestTokenBucket_AllowAndExhaust(t *testing.T) {
	tb := newTokenBucket(5)
	// First 5 calls should be allowed (initial burst = capacity)
	for i := 0; i < 5; i++ {
		if !tb.allow() {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	// 6th call should be denied (bucket empty)
	if tb.allow() {
		t.Fatal("6th call should be rate-limited")
	}
}

func TestTokenBucket_Refills(t *testing.T) {
	tb := newTokenBucket(100)
	// Drain the bucket
	for i := 0; i < 100; i++ {
		tb.allow()
	}
	if tb.allow() {
		t.Fatal("should be rate-limited after draining")
	}
	// Wait for refill (1 token per 10ms at 100/s)
	time.Sleep(20 * time.Millisecond)
	if !tb.allow() {
		t.Fatal("should be allowed after refill window")
	}
}

func TestPeerRateLimiter_DifferentPeers(t *testing.T) {
	limiter := newPeerRateLimiter(2)
	// Two calls from peer A — should both be allowed
	for i := 0; i < 2; i++ {
		if !limiter.allow(peerCtx("192.0.2.1:1234")) {
			t.Fatalf("peer A call %d should be allowed", i+1)
		}
	}
	// Third call from peer A — denied
	if limiter.allow(peerCtx("192.0.2.1:5678")) {
		t.Fatal("peer A 3rd call should be denied")
	}
	// First call from peer B — should be allowed (separate bucket)
	if !limiter.allow(peerCtx("192.0.2.2:1111")) {
		t.Fatal("peer B first call should be allowed")
	}
}

func TestRateLimitUnaryInterceptor_Disabled(t *testing.T) {
	interceptor := rateLimitUnaryInterceptor(0)
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	_, err := interceptor(context.Background(), nil, nil, handler)
	if err != nil {
		t.Fatalf("disabled interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestRateLimitUnaryInterceptor_Enforces(t *testing.T) {
	interceptor := rateLimitUnaryInterceptor(1)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil }
	ctx := peerCtx("10.0.0.1:9999")
	// First call allowed
	if _, err := interceptor(ctx, nil, nil, handler); err != nil {
		t.Fatalf("first call should be allowed: %v", err)
	}
	// Second call immediately — denied
	if _, err := interceptor(ctx, nil, nil, handler); err == nil {
		t.Fatal("second immediate call should be rate-limited")
	}
}
