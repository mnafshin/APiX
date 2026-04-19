package engine

import (
	"context"
	"net/http"

	"github.com/mnafshin/apix/internal/breakpoints"
)

// BreakpointEvaluator is the interface Engine uses to manage breakpoints.
// *breakpoints.Manager satisfies this interface.
type BreakpointEvaluator interface {
	Evaluate(method, rawURL string) string
	EvaluateRequest(method, rawURL string, headers http.Header, body []byte) string
	EvaluateResponse(method, rawURL string, headers http.Header, body []byte, statusCode int) string
	Pause(ctx context.Context, entry *breakpoints.PausedEntry) (*breakpoints.ResumeDecision, error)
	Resume(requestID string, decision *breakpoints.ResumeDecision) error
	AddRule(rule *breakpoints.BreakpointRule) (*breakpoints.BreakpointRule, error)
	RemoveRule(id string) error
	ListRules() []*breakpoints.BreakpointRule
	Subscribe() chan *breakpoints.PausedEntry
	Unsubscribe(ch chan *breakpoints.PausedEntry)
}
