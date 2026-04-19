package replay

import (
	"context"
	"time"
)

// Scenario is an ordered list of replay steps that are executed sequentially.
type Scenario struct {
	Name     string
	Requests []ScenarioStep
}

// ScenarioStep describes a single replay step within a Scenario.
type ScenarioStep struct {
	// RequestID references a transaction stored in the engine's DB.
	RequestID string

	// DelayBefore is an optional pause inserted before this step is executed.
	DelayBefore time.Duration

	// OverrideHeaders are merged on top of the stored request's headers for
	// this step only.
	OverrideHeaders map[string]string
}

// ScenarioResult captures the outcome of a single ScenarioStep.
type ScenarioResult struct {
	StepIndex  int
	RequestID  string
	StatusCode int
	Duration   time.Duration
	Error      string
}

// RunScenario replays every step in s sequentially. DelayBefore is honoured
// for each step. Results are collected for all steps; a step failure does not
// abort the remaining steps.
func (e *Engine) RunScenario(ctx context.Context, s Scenario) []ScenarioResult {
	results := make([]ScenarioResult, 0, len(s.Requests))

	for i, step := range s.Requests {
		// Honour the per-step delay (respects context cancellation).
		if step.DelayBefore > 0 {
			select {
			case <-ctx.Done():
				results = append(results, ScenarioResult{
					StepIndex: i,
					RequestID: step.RequestID,
					Error:     ctx.Err().Error(),
				})
				return results
			case <-time.After(step.DelayBefore):
			}
		}

		start := time.Now()
		resp, err := e.ReplayRequest(ctx, &ReplayRequest{
			RequestID:       step.RequestID,
			OverrideHeaders: step.OverrideHeaders,
		})
		elapsed := time.Since(start)

		result := ScenarioResult{
			StepIndex: i,
			RequestID: step.RequestID,
			Duration:  elapsed,
		}

		if err != nil {
			result.Error = err.Error()
		} else {
			result.StatusCode = resp.StatusCode
			_ = resp.Body.Close()
		}

		results = append(results, result)
	}

	return results
}
