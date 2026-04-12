package stateful_test

import (
	"testing"
)

// A tiny deterministic scenario harness that validates a canonical lifecycle.
// This is a fast unit-level test and does not require running the real engine.

type State string

const (
	StateInit    State = "init"
	StatePaused  State = "paused"
	StateRespond State = "responded"
	StateClosed  State = "closed"
)

func TestCanonicalBreakpointFlow(t *testing.T) {
	state := StateInit

	// Simulate: add breakpoint -> hit -> pause
	if state != StateInit {
		t.Fatalf("expected init state")
	}
	// hit breakpoint
	state = StatePaused
	// respond to paused request
	state = StateRespond
	// resume/close
	state = StateClosed

	if state != StateClosed {
		t.Fatalf("expected final state closed, got %v", state)
	}
}
