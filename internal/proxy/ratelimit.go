package proxy

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mnafshin/apix/internal/config"
)

type tokenBucket struct {
	mu        sync.Mutex
	tokens    float64
	maxTokens float64
	rate      float64
	lastTime  time.Time
}

func newTokenBucket(ratePerSec int) *tokenBucket {
	return &tokenBucket{
		tokens:    float64(ratePerSec),
		maxTokens: float64(ratePerSec),
		rate:      float64(ratePerSec) / float64(time.Second),
		lastTime:  time.Now(),
	}
}

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

type proxyRateLimiter struct {
	ratePerSec    int
	maxConcurrent int

	mu      sync.Mutex
	buckets map[string]*tokenBucket
	active  map[string]int
}

func newProxyRateLimiter(cfg *config.Config) *proxyRateLimiter {
	if cfg == nil {
		return nil
	}
	return &proxyRateLimiter{
		ratePerSec:    cfg.ProxyRateLimitPerSec,
		maxConcurrent: cfg.ProxyMaxConcurrentConnections,
		buckets:       make(map[string]*tokenBucket),
		active:        make(map[string]int),
	}
}

func (l *proxyRateLimiter) allow(clientIP string) bool {
	if l == nil || l.ratePerSec <= 0 {
		return true
	}

	clientIP = normalizeClientIP(clientIP)
	l.mu.Lock()
	bucket, ok := l.buckets[clientIP]
	if !ok {
		bucket = newTokenBucket(l.ratePerSec)
		l.buckets[clientIP] = bucket
	}
	l.mu.Unlock()

	return bucket.allow()
}

func (l *proxyRateLimiter) acquire(clientIP string) bool {
	if l == nil || l.maxConcurrent <= 0 {
		return true
	}

	clientIP = normalizeClientIP(clientIP)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[clientIP] >= l.maxConcurrent {
		return false
	}
	l.active[clientIP]++
	return true
}

func (l *proxyRateLimiter) release(clientIP string) {
	if l == nil || l.maxConcurrent <= 0 {
		return
	}
	clientIP = normalizeClientIP(clientIP)
	l.mu.Lock()
	defer l.mu.Unlock()
	if current := l.active[clientIP]; current <= 1 {
		delete(l.active, clientIP)
	} else {
		l.active[clientIP] = current - 1
	}
}

func normalizeClientIP(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
