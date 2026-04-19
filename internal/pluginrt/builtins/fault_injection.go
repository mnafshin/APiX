package builtins

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// FaultRule defines a single fault injection rule.
type FaultRule struct {
	// MatchPath is a substring matched against the request URL path. Empty matches all.
	MatchPath string
	// MatchMethod is the HTTP method to match (e.g. "GET"). Empty matches all.
	MatchMethod string
	// Percentage is the probability (0.0–100.0) that a matching request is affected.
	Percentage float64
	// FaultType is one of "abort", "delay", or "header".
	FaultType string
	// AbortStatus is the HTTP status code returned for abort faults (default 503).
	AbortStatus int
	// DelayDuration is how long to sleep for delay faults.
	DelayDuration time.Duration
	// HeaderName is the header to inject for header faults.
	HeaderName string
	// HeaderValue is the value of the injected header.
	HeaderValue string
}

// FaultInjection is a chaos/fault injection plugin that can abort requests,
// introduce latency, or inject headers based on configurable rules.
type FaultInjection struct {
	Rules []FaultRule
}

func (p *FaultInjection) Name() string    { return "fault-injection" }
func (p *FaultInjection) Version() string { return "1.0.0" }
func (p *FaultInjection) Description() string {
	return "Chaos/fault injection: abort, delay, or inject headers for matched requests."
}

func (p *FaultInjection) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	modified := false
	clone := req.Clone(req.Body)

	for _, rule := range p.Rules {
		if !matchesRule(rule, req) {
			continue
		}
		if rule.Percentage < 100.0 && rand.Float64()*100.0 >= rule.Percentage { //nolint:gosec
			continue
		}

		switch rule.FaultType {
		case "abort":
			status := rule.AbortStatus
			if status == 0 {
				status = http.StatusServiceUnavailable
			}
			body := []byte("fault injected")
			clone.MockedResponse = &plugins.ProxyResponse{
				StatusCode: status,
				Status:     http.StatusText(status),
				Headers:    make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
			}
			modified = true

		case "delay":
			time.Sleep(rule.DelayDuration)
			modified = true

		case "header":
			if rule.HeaderName != "" {
				// Re-read and re-attach body so clone has a fresh reader.
				body, err := io.ReadAll(clone.Body)
				if err != nil {
					return nil, err
				}
				clone = clone.Clone(io.NopCloser(bytes.NewReader(body)))
				clone.Headers.Set(rule.HeaderName, rule.HeaderValue)
				modified = true
			}
		}
	}

	if !modified {
		return nil, nil
	}
	return clone, nil
}

func (p *FaultInjection) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}

func matchesRule(rule FaultRule, req *plugins.ProxyRequest) bool {
	if rule.MatchPath != "" && !strings.Contains(req.URL.Path, rule.MatchPath) {
		return false
	}
	if rule.MatchMethod != "" && !strings.EqualFold(req.Method, rule.MatchMethod) {
		return false
	}
	return true
}
