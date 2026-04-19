package builtins

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/mnafshin/apix/pkg/plugins"
)

// PolicyEngineConfig holds the configuration for the PolicyEngine plugin.
type PolicyEngineConfig struct {
	// Rules are evaluated in order; the first match wins.
	Rules []PolicyRule
	// DefaultAction is applied when no rule matches. Must be "allow" or "deny".
	// Defaults to "allow" when empty.
	DefaultAction string
}

// PolicyRule is a named rule with an action and a set of conditions (AND logic).
type PolicyRule struct {
	// Name is a human-readable identifier for logging.
	Name string
	// Action is one of: "allow", "deny", "log", "enrich".
	Action string
	// Conditions are evaluated with AND semantics; all must match.
	Conditions []Condition
	// Headers are added to the request for the "enrich" action.
	Headers map[string]string
}

// Condition matches a single field of the incoming request.
type Condition struct {
	// Field is one of: "path", "method", "host", "header:<name>", "query:<name>".
	Field string
	// Operator is one of: "eq", "neq", "contains", "not_contains", "prefix", "suffix", "regex".
	Operator string
	// Value is the comparison target.
	Value string
}

// PolicyEngine enforces access-control and enrichment rules on every request.
// Rules are read-only after construction, so no mutex is needed.
type PolicyEngine struct {
	cfg      PolicyEngineConfig
	compiled map[int]map[int]*regexp.Regexp // rule index → condition index → compiled regex
}

// NewPolicyEngine creates a PolicyEngine from the supplied config, pre-compiling
// all regex conditions. Returns an error if any regex is invalid.
func NewPolicyEngine(cfg PolicyEngineConfig) (*PolicyEngine, error) {
	if cfg.DefaultAction == "" {
		cfg.DefaultAction = "allow"
	}
	compiled := make(map[int]map[int]*regexp.Regexp)
	for ri, rule := range cfg.Rules {
		for ci, cond := range rule.Conditions {
			if cond.Operator == "regex" {
				re, err := regexp.Compile(cond.Value)
				if err != nil {
					return nil, fmt.Errorf("policy rule %q condition %d: invalid regex %q: %w", rule.Name, ci, cond.Value, err)
				}
				if compiled[ri] == nil {
					compiled[ri] = make(map[int]*regexp.Regexp)
				}
				compiled[ri][ci] = re
			}
		}
	}
	return &PolicyEngine{cfg: cfg, compiled: compiled}, nil
}

func (p *PolicyEngine) Name() string        { return "policy-engine" }
func (p *PolicyEngine) Version() string     { return "1.0.0" }
func (p *PolicyEngine) Description() string { return "Enforce allow/deny/log/enrich rules on incoming requests." }

func (p *PolicyEngine) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	for ri, rule := range p.cfg.Rules {
		if !p.allConditionsMatch(ri, rule.Conditions, req) {
			continue
		}
		return p.applyAction(rule, req)
	}
	// No rule matched — apply default action.
	defaultRule := PolicyRule{Name: "<default>", Action: p.cfg.DefaultAction}
	return p.applyAction(defaultRule, req)
}

func (p *PolicyEngine) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}

// allConditionsMatch returns true when every condition in the slice matches the request.
func (p *PolicyEngine) allConditionsMatch(ruleIdx int, conds []Condition, req *plugins.ProxyRequest) bool {
	for ci, cond := range conds {
		val := p.extractField(cond.Field, req)
		var re *regexp.Regexp
		if m, ok := p.compiled[ruleIdx]; ok {
			re = m[ci]
		}
		if !evalCondition(cond.Operator, val, cond.Value, re) {
			return false
		}
	}
	return true
}

// extractField returns the request value identified by field.
func (p *PolicyEngine) extractField(field string, req *plugins.ProxyRequest) string {
	switch {
	case field == "path":
		return req.URL.Path
	case field == "method":
		return req.Method
	case field == "host":
		return req.URL.Host
	case strings.HasPrefix(field, "header:"):
		name := strings.TrimPrefix(field, "header:")
		return req.Headers.Get(name)
	case strings.HasPrefix(field, "query:"):
		name := strings.TrimPrefix(field, "query:")
		return req.URL.Query().Get(name)
	default:
		return ""
	}
}

// evalCondition tests a single (operator, actual, expected) triple.
func evalCondition(operator, actual, expected string, re *regexp.Regexp) bool {
	switch operator {
	case "eq":
		return actual == expected
	case "neq":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	case "not_contains":
		return !strings.Contains(actual, expected)
	case "prefix":
		return strings.HasPrefix(actual, expected)
	case "suffix":
		return strings.HasSuffix(actual, expected)
	case "regex":
		if re == nil {
			return false
		}
		return re.MatchString(actual)
	default:
		return false
	}
}

// applyAction executes rule.Action and returns the (possibly modified) request.
func (p *PolicyEngine) applyAction(rule PolicyRule, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	switch rule.Action {
	case "allow":
		return nil, nil

	case "deny":
		body := []byte("Forbidden by policy")
		clone := req.Clone(req.Body)
		clone.MockedResponse = &plugins.ProxyResponse{
			StatusCode: http.StatusForbidden,
			Status:     http.StatusText(http.StatusForbidden),
			Headers:    make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
		}
		return clone, nil

	case "log":
		slog.Info("policy-engine: rule matched — action: log (pass-through)", "rule", rule.Name)
		return nil, nil

	case "enrich":
		if len(rule.Headers) == 0 {
			return nil, nil
		}
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		clone := req.Clone(io.NopCloser(bytes.NewReader(bodyBytes)))
		for k, v := range rule.Headers {
			clone.Headers.Set(k, v)
		}
		return clone, nil

	default:
		// Unknown action — pass through and log.
		slog.Warn("policy-engine: unknown action — passing through", "action", rule.Action, "rule", rule.Name)
		return nil, nil
	}
}
