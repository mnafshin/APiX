package builtins

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// RetryPolicyConfig holds the full configuration for the RetryPolicy plugin.
type RetryPolicyConfig struct {
	Rules []RetryRule
}

// RetryRule describes a single retry policy matched against a request.
type RetryRule struct {
	// MatchPath is a path substring filter; empty matches all paths.
	MatchPath string
	// MatchMethods lists the HTTP methods eligible for retry.
	// Defaults to GET, HEAD, OPTIONS when nil/empty.
	MatchMethods []string
	// RetryOnStatus lists status codes that should trigger a retry.
	RetryOnStatus []int
	// MaxAttempts is the maximum number of retry attempts (default 3).
	MaxAttempts int
	// Backoff is "fixed" or "exponential" (default "exponential").
	Backoff string
	// BackoffBase is the initial sleep duration (default 100ms).
	BackoffBase time.Duration
	// BackoffMax caps the sleep duration (default 5s).
	BackoffMax time.Duration
}

var defaultMatchMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions}

func (r *RetryRule) maxAttempts() int {
	if r.MaxAttempts <= 0 {
		return 3
	}
	return r.MaxAttempts
}

func (r *RetryRule) backoffBase() time.Duration {
	if r.BackoffBase <= 0 {
		return 100 * time.Millisecond
	}
	return r.BackoffBase
}

func (r *RetryRule) backoffMax() time.Duration {
	if r.BackoffMax <= 0 {
		return 5 * time.Second
	}
	return r.BackoffMax
}

func (r *RetryRule) effectiveMethods() []string {
	if len(r.MatchMethods) == 0 {
		return defaultMatchMethods
	}
	return r.MatchMethods
}

// sleepDuration returns the backoff duration for attempt n (0-indexed).
func (r *RetryRule) sleepDuration(attempt int) time.Duration {
	base := r.backoffBase()
	max := r.backoffMax()
	var d time.Duration
	if strings.EqualFold(r.Backoff, "fixed") {
		d = base
	} else {
		// exponential: base * 2^attempt
		d = time.Duration(float64(base) * math.Pow(2, float64(attempt)))
	}
	if d > max {
		return max
	}
	return d
}

// matchesMethod reports whether method is in the rule's effective method list.
func (r *RetryRule) matchesMethod(method string) bool {
	for _, m := range r.effectiveMethods() {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// matchesStatus reports whether status is in RetryOnStatus.
func (r *RetryRule) matchesStatus(status int) bool {
	for _, s := range r.RetryOnStatus {
		if s == status {
			return true
		}
	}
	return false
}

// matchesPath reports whether the rule applies to the given path.
func (r *RetryRule) matchesPath(path string) bool {
	if r.MatchPath == "" {
		return true
	}
	return strings.Contains(path, r.MatchPath)
}

// RetryPolicy is an APiX plugin that retries failed upstream requests
// according to configurable rules.
type RetryPolicy struct {
	RetryPolicyConfig
}

func (p *RetryPolicy) Name() string        { return "retry-policy" }
func (p *RetryPolicy) Version() string     { return "1.0.0" }
func (p *RetryPolicy) Description() string { return "Retries upstream requests on configurable status codes." }

// OnRequest is a no-op; retry logic lives in OnResponse.
func (p *RetryPolicy) OnRequest(_ context.Context, _ *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	return nil, nil
}

// OnResponse retries the request when the response status matches a rule.
// Returns a replacement ProxyResponse after exhausting retries, or nil when
// no rule applies.
func (p *RetryPolicy) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if req.Raw == nil {
		return nil, nil
	}

	path := req.URL.Path
	method := req.Method

	for i := range p.Rules {
		rule := &p.Rules[i]
		if !rule.matchesPath(path) {
			continue
		}
		if !rule.matchesMethod(method) {
			continue
		}
		if !rule.matchesStatus(resp.StatusCode) {
			continue
		}
		maxAttempts := rule.MaxAttempts
		if maxAttempts <= 0 {
			continue
		}

		current := resp
		for attempt := 0; attempt < maxAttempts; attempt++ {
			sleep := rule.sleepDuration(attempt)
			select {
			case <-ctx.Done():
				return current, nil
			case <-time.After(sleep):
			}

			httpResp, err := http.DefaultClient.Do(req.Raw.Clone(context.Background()))
			if err != nil {
				// Network error — stop retrying and return last known response.
				return current, nil
			}
			current = proxyResponseFromHTTP(httpResp)

			if !rule.matchesStatus(current.StatusCode) {
				// Retry succeeded — return the good response.
				return current, nil
			}
		}
		// Exhausted retries — return whatever we have.
		return current, nil
	}

	return nil, nil
}

// proxyResponseFromHTTP converts a standard *http.Response to *plugins.ProxyResponse.
func proxyResponseFromHTTP(r *http.Response) *plugins.ProxyResponse {
	headers := make(http.Header, len(r.Header))
	for k, vs := range r.Header {
		headers[k] = append([]string(nil), vs...)
	}
	body := r.Body
	if body == nil {
		body = io.NopCloser(strings.NewReader(""))
	}
	return &plugins.ProxyResponse{
		StatusCode: r.StatusCode,
		Status:     r.Status,
		Headers:    headers,
		Body:       body,
		Raw:        r,
	}
}
