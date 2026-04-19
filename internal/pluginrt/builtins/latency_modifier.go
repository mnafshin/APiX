package builtins

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/mnafshin/apix/pkg/plugins"
)

// LatencyRule defines a single latency/response-modification rule.
type LatencyRule struct {
	// MatchPath is a substring matched against the request URL path. Empty matches all.
	MatchPath string
	// MatchMethod is the HTTP method to match (e.g. "GET"). Empty matches all.
	MatchMethod string
	// FixedDelay is a deterministic delay applied before forwarding the request.
	FixedDelay time.Duration
	// JitterMax adds a random extra delay in [0, JitterMax]. Ignored when zero.
	JitterMax time.Duration
	// AddHeaders are response headers to add or overwrite.
	AddHeaders map[string]string
	// RemoveHeaders are response header names to strip.
	RemoveHeaders []string
	// StatusRemap remaps upstream response status codes (e.g. 200 → 418).
	StatusRemap map[int]int
}

// LatencyModifierConfig holds the full set of rules for the plugin.
type LatencyModifierConfig struct {
	Rules []LatencyRule
}

// LatencyModifier is a plugin that injects latency into matching requests and
// applies header/status mutations to matching responses.
type LatencyModifier struct {
	cfg LatencyModifierConfig
}

// NewLatencyModifier constructs a LatencyModifier with the provided config.
func NewLatencyModifier(cfg LatencyModifierConfig) *LatencyModifier {
	return &LatencyModifier{cfg: cfg}
}

func (p *LatencyModifier) Name() string    { return "latency-modifier" }
func (p *LatencyModifier) Version() string { return "1.0.0" }
func (p *LatencyModifier) Description() string {
	return "Inject fixed/jitter latency on requests and mutate response headers/status codes."
}

// OnRequest sleeps for FixedDelay + rand[0,JitterMax) on each matching rule.
func (p *LatencyModifier) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	for _, rule := range p.cfg.Rules {
		if !latencyRuleMatches(rule, req) {
			continue
		}
		delay := rule.FixedDelay
		if rule.JitterMax > 0 {
			delay += time.Duration(rand.Int63n(int64(rule.JitterMax) + 1))
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, nil
}

// OnResponse applies AddHeaders, RemoveHeaders, and StatusRemap for matching rules.
func (p *LatencyModifier) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	modified := false

	// Read body once so we can clone safely if needed.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	clone := resp.Clone(io.NopCloser(bytes.NewReader(body)))

	for _, rule := range p.cfg.Rules {
		if !latencyRuleMatches(rule, req) {
			continue
		}

		for _, h := range rule.RemoveHeaders {
			if clone.Headers.Get(h) != "" {
				clone.Headers.Del(h)
				modified = true
			}
		}

		for k, v := range rule.AddHeaders {
			clone.Headers.Set(k, v)
			modified = true
		}

		if newStatus, ok := rule.StatusRemap[clone.StatusCode]; ok {
			clone.StatusCode = newStatus
			modified = true
		}
	}

	if !modified {
		return nil, nil
	}
	return clone, nil
}

func latencyRuleMatches(rule LatencyRule, req *plugins.ProxyRequest) bool {
	if rule.MatchPath != "" && !strings.Contains(req.URL.Path, rule.MatchPath) {
		return false
	}
	if rule.MatchMethod != "" && !strings.EqualFold(req.Method, rule.MatchMethod) {
		return false
	}
	return true
}
