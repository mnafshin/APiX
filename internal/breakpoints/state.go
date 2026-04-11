package breakpoints

import (
	"net/http"
	"time"
)

// BreakpointState represents the lifecycle of a single paused request.
type BreakpointState int

const (
	// StatePaused: request is being held, awaiting a resume decision.
	StatePaused BreakpointState = iota
	// StateResumed: decision received, request is being forwarded.
	StateResumed
	// StateDropped: decision received, request will be discarded (502).
	StateDropped
	// StateResponded: decision received, a synthetic response will be returned.
	StateResponded
	// StateExpired: no decision received within the timeout window.
	StateExpired
)

// BreakpointRule defines criteria for pausing requests.
type BreakpointRule struct {
	ID         string
	URLPattern string   // regex pattern matched against full URL
	Methods    []string // nil/empty = all methods
	Enabled    bool
	Label      string
}

// PausedEntry represents a request currently held at a breakpoint.
type PausedEntry struct {
	RequestID    string
	BreakpointID string
	Request      *http.Request // snapshot of the original request
	PausedAt     time.Time
	State        BreakpointState
	done         chan struct{}   // closed when a decision is made
	decision     *ResumeDecision // set before closing done
}

// ResumeDecision carries the action and optional modifications chosen by the user.
type ResumeDecision struct {
	Action           ResumeAction
	ModifiedRequest  *http.Request  // nil = forward original
	ModifiedResponse *http.Response // only when Action == ActionRespond
}

// ResumeAction mirrors the proto enum at the domain layer.
type ResumeAction int

const (
	ActionForward ResumeAction = iota // forward (optionally with modifications)
	ActionDrop                        // drop; return 502 to client
	ActionRespond                     // return synthetic response to client
)

// NewPausedEntry initialises a PausedEntry for requestID matching breakpointID.
func NewPausedEntry(requestID, breakpointID string, req *http.Request) *PausedEntry {
	return &PausedEntry{
		RequestID:    requestID,
		BreakpointID: breakpointID,
		Request:      req,
		PausedAt:     time.Now(),
		State:        StatePaused,
		done:         make(chan struct{}),
	}
}
