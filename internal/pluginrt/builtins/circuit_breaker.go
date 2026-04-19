package builtins

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// cbState represents the circuit breaker state machine state.
type cbState int

const (
	cbClosed   cbState = iota // normal operation, requests pass through
	cbOpen                    // failing, requests are rejected with 503
	cbHalfOpen                // probing, one request allowed through
)

// breaker holds per-host circuit breaker state.
type breaker struct {
	mu              sync.Mutex
	state           cbState
	failures        int
	successes       int
	openedAt        time.Time
	halfOpenPending bool // true when a probe request has been let through
}

// CircuitBreaker is a per-host circuit breaker plugin.
//
// States: CLOSED (normal) → OPEN (failing) → HALF_OPEN (probing) → CLOSED
//
// Configuration:
//
//	FailureThreshold  consecutive 5xx responses before opening (default 5)
//	SuccessThreshold  consecutive non-5xx responses in HALF_OPEN to close (default 2)
//	OpenTimeout       time to stay OPEN before moving to HALF_OPEN (default 30s)
//	MatchHosts        hosts to protect; empty means all hosts
type CircuitBreaker struct {
	FailureThreshold int
	SuccessThreshold int
	OpenTimeout      time.Duration
	MatchHosts       []string

	mu       sync.Mutex
	breakers map[string]*breaker
}

func (p *CircuitBreaker) Name() string    { return "circuit-breaker" }
func (p *CircuitBreaker) Version() string { return "1.0.0" }
func (p *CircuitBreaker) Description() string {
	return "Per-host circuit breaker: open after consecutive failures, close after successful probes."
}

func (p *CircuitBreaker) failureThreshold() int {
	if p.FailureThreshold <= 0 {
		return 5
	}
	return p.FailureThreshold
}

func (p *CircuitBreaker) successThreshold() int {
	if p.SuccessThreshold <= 0 {
		return 2
	}
	return p.SuccessThreshold
}

func (p *CircuitBreaker) openTimeout() time.Duration {
	if p.OpenTimeout <= 0 {
		return 30 * time.Second
	}
	return p.OpenTimeout
}

// matchesHost reports whether the plugin applies to the given host.
func (p *CircuitBreaker) matchesHost(host string) bool {
	if len(p.MatchHosts) == 0 {
		return true
	}
	for _, h := range p.MatchHosts {
		if h == host {
			return true
		}
	}
	return false
}

// getBreaker returns the breaker for host, creating it if needed.
func (p *CircuitBreaker) getBreaker(host string) *breaker {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.breakers == nil {
		p.breakers = make(map[string]*breaker)
	}
	b, ok := p.breakers[host]
	if !ok {
		b = &breaker{}
		p.breakers[host] = b
	}
	return b
}

// OnRequest intercepts the request. When the circuit is OPEN it returns a
// synthetic 503 Service Unavailable response without contacting upstream.
// When HALF_OPEN it allows the first probe request through and blocks the rest.
func (p *CircuitBreaker) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	host := req.URL.Hostname()
	if !p.matchesHost(host) {
		return nil, nil
	}

	b := p.getBreaker(host)
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case cbOpen:
		if time.Since(b.openedAt) >= p.openTimeout() {
			// Transition to HALF_OPEN and allow probe through.
			b.state = cbHalfOpen
			b.successes = 0
			b.halfOpenPending = true
			return nil, nil
		}
		// Still OPEN — short-circuit with 503.
		return p.blocked503(req), nil

	case cbHalfOpen:
		if b.halfOpenPending {
			// A probe is already in-flight; block additional requests.
			return p.blocked503(req), nil
		}
		// Allow another probe (covers the case where the previous probe
		// succeeded and incremented successes but hasn't closed yet).
		b.halfOpenPending = true
		return nil, nil

	default: // cbClosed
		return nil, nil
	}
}

// OnResponse records success or failure based on the response status code
// and drives the state machine transitions.
func (p *CircuitBreaker) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	host := req.URL.Hostname()
	if !p.matchesHost(host) {
		return nil, nil
	}

	b := p.getBreaker(host)
	b.mu.Lock()
	defer b.mu.Unlock()

	isFailure := resp.StatusCode >= 500

	switch b.state {
	case cbClosed:
		if isFailure {
			b.failures++
			if b.failures >= p.failureThreshold() {
				b.state = cbOpen
				b.openedAt = time.Now()
				b.failures = 0
			}
		} else {
			b.failures = 0
		}

	case cbHalfOpen:
		b.halfOpenPending = false
		if isFailure {
			// Probe failed — re-open.
			b.state = cbOpen
			b.openedAt = time.Now()
			b.successes = 0
		} else {
			b.successes++
			if b.successes >= p.successThreshold() {
				b.state = cbClosed
				b.successes = 0
				b.failures = 0
			}
		}
	}

	return nil, nil
}

// blocked503 constructs a synthetic 503 Service Unavailable response.
func (p *CircuitBreaker) blocked503(req *plugins.ProxyRequest) *plugins.ProxyRequest {
	body := fmt.Sprintf("circuit breaker open for host %s", req.URL.Hostname())
	hdrs := make(http.Header)
	hdrs.Set("Content-Type", "text/plain")

	clone := req.Clone(req.Body)
	clone.MockedResponse = &plugins.ProxyResponse{
		StatusCode: http.StatusServiceUnavailable,
		Status:     http.StatusText(http.StatusServiceUnavailable),
		Headers:    hdrs,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
	return clone
}
