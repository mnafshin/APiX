package builtins

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// RateLimiterConfig holds the list of rate rules applied in order.
type RateLimiterConfig struct {
	Rules []RateRule
}

// RateRule defines a single token-bucket rate limit applied to a subset of traffic.
type RateRule struct {
	// KeySource determines how the bucket key is derived:
	//   "ip"             — remote IP address
	//   "header:<name>"  — value of the named request header
	//   "path"           — request URL path
	//   "global"         — single shared bucket for all requests
	KeySource string
	// MatchPath, when non-empty, restricts the rule to requests whose path
	// contains this substring.
	MatchPath string
	// Rate is the token refill rate in tokens per second.
	Rate float64
	// Burst is the maximum bucket capacity (also the initial token count).
	Burst int
	// RejectWith is the HTTP status code returned when the bucket is empty.
	// Defaults to 429 when zero.
	RejectWith int
}

// bucket is a single token-bucket counter, safe for concurrent use.
type bucket struct {
	mu       sync.Mutex
	tokens   float64
	lastTime time.Time
	rate     float64
	burst    int
}

// allow refills the bucket based on elapsed time then attempts to consume one
// token. Returns true if a token was available, false if the limit is exceeded.
func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.lastTime = now

	// Refill tokens based on elapsed time.
	b.tokens += elapsed * b.rate
	if b.tokens > float64(b.burst) {
		b.tokens = float64(b.burst)
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimiter is a per-key token-bucket rate limiter plugin.
//
// For each incoming request it evaluates rules in order. The first rule that
// matches the request path (or matches all paths when MatchPath is empty) and
// finds no tokens available synthesises a rejection response and short-circuits
// further processing.
type RateLimiter struct {
	cfg     RateLimiterConfig
	mu      sync.Mutex
	buckets map[string]*bucket // "ruleIndex:key" → bucket
}

// NewRateLimiter constructs a RateLimiter from the provided config.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*bucket),
	}
}

func (p *RateLimiter) Name() string        { return "rate-limiter" }
func (p *RateLimiter) Version() string     { return "1.0.0" }
func (p *RateLimiter) Description() string {
	return "Per-key token-bucket rate limiter with configurable key sources and rejection status."
}

// getBucket returns (or lazily creates) the bucket for the given composite key.
func (p *RateLimiter) getBucket(key string, rule RateRule) *bucket {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   float64(rule.Burst),
			lastTime: time.Now(),
			rate:     rule.Rate,
			burst:    rule.Burst,
		}
		p.buckets[key] = b
	}
	return b
}

// extractKey derives the bucket key for the given rule and request.
func extractKey(rule RateRule, req *plugins.ProxyRequest) string {
	switch {
	case rule.KeySource == "global":
		return "global"

	case rule.KeySource == "ip":
		if req.Raw == nil {
			return "unknown"
		}
		host, _, err := net.SplitHostPort(req.Raw.RemoteAddr)
		if err != nil {
			return "unknown"
		}
		return host

	case strings.HasPrefix(rule.KeySource, "header:"):
		name := strings.TrimPrefix(rule.KeySource, "header:")
		val := req.Headers.Get(name)
		if val == "" {
			return "unknown"
		}
		return val

	case rule.KeySource == "path":
		return req.URL.Path

	default:
		return "unknown"
	}
}

// OnRequest checks each rule against the incoming request. The first rule that
// matches and has an empty bucket sets MockedResponse to a rejection response.
func (p *RateLimiter) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	for i, rule := range p.cfg.Rules {
		// Path filter: skip if the rule targets a specific path and this
		// request's path does not contain it.
		if rule.MatchPath != "" && !strings.Contains(req.URL.Path, rule.MatchPath) {
			continue
		}

		key := fmt.Sprintf("%d:%s", i, extractKey(rule, req))
		b := p.getBucket(key, rule)

		if !b.allow() {
			rejectStatus := rule.RejectWith
			if rejectStatus == 0 {
				rejectStatus = http.StatusTooManyRequests
			}

			body := io.NopCloser(strings.NewReader("rate limit exceeded"))
			clone := req.Clone(req.Body)
			clone.MockedResponse = &plugins.ProxyResponse{
				StatusCode: rejectStatus,
				Status:     http.StatusText(rejectStatus),
				Headers:    http.Header{"Retry-After": []string{"1"}},
				Body:       body,
			}
			return clone, nil
		}
	}
	return nil, nil
}

// OnResponse is a no-op; rate limiting decisions are made at request time.
func (p *RateLimiter) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}
