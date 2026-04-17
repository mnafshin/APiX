package server

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// tokenBucket is a simple per-peer token bucket.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxTokens float64
	rate     float64 // tokens per nanosecond
	lastTime time.Time
}

func newTokenBucket(ratePerSec int) *tokenBucket {
	return &tokenBucket{
		tokens:    float64(ratePerSec),
		maxTokens: float64(ratePerSec),
		rate:      float64(ratePerSec) / float64(time.Second),
		lastTime:  time.Now(),
	}
}

// allow returns true if a token is available (and consumes it).
func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastTime)
	tb.lastTime = now
	tb.tokens += float64(elapsed) * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}

// peerRateLimiter tracks one token bucket per remote peer address.
type peerRateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	ratePerSec  int
}

func newPeerRateLimiter(ratePerSec int) *peerRateLimiter {
	return &peerRateLimiter{
		buckets:    make(map[string]*tokenBucket),
		ratePerSec: ratePerSec,
	}
}

func (l *peerRateLimiter) allow(ctx context.Context) bool {
	addr := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		addr = p.Addr.String()
	}
	// Strip port so all connections from the same host share a bucket.
	if host, _, err := splitHostPort(addr); err == nil {
		addr = host
	}

	l.mu.Lock()
	b, ok := l.buckets[addr]
	if !ok {
		b = newTokenBucket(l.ratePerSec)
		l.buckets[addr] = b
	}
	l.mu.Unlock()
	return b.allow()
}

// splitHostPort splits host:port, returning host. Falls back to the full
// address if net.SplitHostPort fails.
func splitHostPort(addr string) (host, port string, err error) {
	// Inline to avoid importing net in this file.
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return addr, "", nil
}

// rateLimitUnaryInterceptor returns a gRPC unary interceptor that enforces
// ratePerSec RPC calls per peer address per second using a token bucket.
// When ratePerSec is 0, the interceptor is a no-op pass-through.
func rateLimitUnaryInterceptor(ratePerSec int) grpc.UnaryServerInterceptor {
	if ratePerSec <= 0 {
		return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return handler(ctx, req)
		}
	}
	limiter := newPeerRateLimiter(ratePerSec)
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !limiter.allow(ctx) {
			return nil, status.Errorf(codes.ResourceExhausted, "rate limit exceeded (%d req/s per client)", ratePerSec)
		}
		return handler(ctx, req)
	}
}

// rateLimitStreamInterceptor returns a gRPC stream interceptor that enforces
// ratePerSec stream opens per peer address per second.
// When ratePerSec is 0, the interceptor is a no-op pass-through.
func rateLimitStreamInterceptor(ratePerSec int) grpc.StreamServerInterceptor {
	if ratePerSec <= 0 {
		return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, ss)
		}
	}
	limiter := newPeerRateLimiter(ratePerSec)
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !limiter.allow(ss.Context()) {
			return status.Errorf(codes.ResourceExhausted, "rate limit exceeded (%d req/s per client)", ratePerSec)
		}
		return handler(srv, ss)
	}
}
