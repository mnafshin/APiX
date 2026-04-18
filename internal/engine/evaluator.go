package engine

import (
	"context"

	"github.com/mnafshin/apix/internal/breakpoints"
)

// BreakpointEvaluator is the interface Engine uses to manage breakpoints.
// *breakpoints.Manager satisfies this interface.
type BreakpointEvaluator interface {
	Evaluate(method, rawURL string) string
	Pause(ctx context.Context, entry *breakpoints.PausedEntry) (*breakpoints.ResumeDecision, error)
	Resume(requestID string, decision *breakpoints.ResumeDecision) error
	AddRule(rule *breakpoints.BreakpointRule) (*breakpoints.BreakpointRule, error)
	RemoveRule(id string) error
	ListRules() []*breakpoints.BreakpointRule
	Subscribe() chan *breakpoints.PausedEntry
	Unsubscribe(ch chan *breakpoints.PausedEntry)
}
