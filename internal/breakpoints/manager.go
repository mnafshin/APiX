package breakpoints

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager maintains a set of BreakpointRules and evaluates incoming requests
// against them. When a request matches, it is held in a paused state until
// the VS Code extension calls ResumeRequest.
type Manager struct {
	mu          sync.RWMutex
	rules       map[string]*BreakpointRule // id → rule
	compiled    map[string]*regexp.Regexp  // id → compiled URLPattern
	pausedReqs  map[string]*PausedEntry    // request_id → entry
	subscribers map[chan *PausedEntry]struct{}
}

// NewManager creates an empty breakpoint manager.
func NewManager() *Manager {
	return &Manager{
		rules:       make(map[string]*BreakpointRule),
		compiled:    make(map[string]*regexp.Regexp),
		pausedReqs:  make(map[string]*PausedEntry),
		subscribers: make(map[chan *PausedEntry]struct{}),
	}
}

// validHTTPMethods is the set of RFC 9110 standard methods.
var validHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// regexEvalSem limits concurrent regexp evaluations to avoid DoS via too many
// simultaneous / expensive matches. Choosing a modest default keeps memory and
// CPU bounded under load while still allowing parallel checks.
var regexEvalSem = make(chan struct{}, 32)

// AddRule registers a new breakpoint rule. Assigns a UUID if rule.ID is empty.
func (m *Manager) AddRule(rule *BreakpointRule) (*BreakpointRule, error) {
	if rule.URLPattern == "" {
		return nil, fmt.Errorf("url_pattern is required")
	}
	if len(rule.URLPattern) > 500 {
		return nil, fmt.Errorf("url_pattern exceeds 500 characters")
	}
	for _, method := range rule.Methods {
		if !validHTTPMethods[strings.ToUpper(method)] {
			return nil, fmt.Errorf("invalid HTTP method: %q", method)
		}
	}
	re, err := regexp.Compile(rule.URLPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid url_pattern %q: %w", rule.URLPattern, err)
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	m.mu.Lock()
	m.rules[rule.ID] = rule
	m.compiled[rule.ID] = re
	m.mu.Unlock()
	return rule, nil
}

// RemoveRule deletes a rule by ID.
func (m *Manager) RemoveRule(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[id]; !ok {
		return fmt.Errorf("breakpoint %q not found", id)
	}
	delete(m.rules, id)
	delete(m.compiled, id)
	return nil
}

// ListRules returns all registered rules.
func (m *Manager) ListRules() []*BreakpointRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rules := make([]*BreakpointRule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	return rules
}

// Evaluate checks req against all enabled rules. Returns the matching rule ID
// or empty string if no rule matches. Uses a timeout to prevent ReDoS attacks.
func (m *Manager) Evaluate(method, rawURL string) string {
	// Timeout to avoid long-running regex matches (protect against ReDoS).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Snapshot rules and compiled regexes while holding the read lock so we
	// don't hold the lock during potentially expensive MatchString calls.
	m.mu.RLock()
	type entry struct {
		id   string
		rule *BreakpointRule
		re   *regexp.Regexp
	}
	entries := make([]entry, 0, len(m.rules))
	for id, r := range m.rules {
		entries = append(entries, entry{id: id, rule: r, re: m.compiled[id]})
	}
	m.mu.RUnlock()

	done := make(chan string, 1)
	go func() {
		for _, e := range entries {
			if e.rule == nil || !e.rule.Enabled {
				continue
			}
			// Check method: empty methods list means match all.
			if len(e.rule.Methods) > 0 {
				matched := false
				for _, mm := range e.rule.Methods {
					if strings.EqualFold(mm, method) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}

			re := e.re
			if re == nil {
				continue
			}

			// Acquire a slot for regex evaluation, respecting the overall ctx timeout.
			select {
			case regexEvalSem <- struct{}{}:
				// acquired
			case <-ctx.Done():
				done <- ""
				return
			}

			// Perform match in a small critical section so we can release the
			// semaphore promptly. If MatchString blocks, the goroutine will run
			// until completion but the semaphore prevents unbounded parallelism.
			matched := false
			func() {
				defer func() { <-regexEvalSem }()
				if re.MatchString(rawURL) {
					matched = true
				}
			}()

			if matched {
				done <- e.id
				return
			}
		}
		done <- ""
	}()

	select {
	case ruleID := <-done:
		return ruleID
	case <-ctx.Done():
		// Timeout or context cancelled: return no match to avoid blocking.
		return ""
	}
}

// Pause holds req until resumed, broadcasting to all Watch subscribers.
// Blocks until ResumeRequest is called for requestID.
func (m *Manager) Pause(ctx context.Context, entry *PausedEntry) (*ResumeDecision, error) {
	m.mu.Lock()
	m.pausedReqs[entry.RequestID] = entry

	// Snapshot subscribers while holding the lock.
	subs := make([]chan *PausedEntry, 0, len(m.subscribers))
	for ch := range m.subscribers {
		subs = append(subs, ch)
	}
	m.mu.Unlock()

	// Broadcast to all subscribers (non-blocking).
	for _, ch := range subs {
		select {
		case ch <- entry:
		default:
		}
	}

	// Block until resumed or context cancelled.
	select {
	case <-entry.done:
		m.mu.Lock()
		delete(m.pausedReqs, entry.RequestID)
		m.mu.Unlock()
		return entry.decision, nil
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pausedReqs, entry.RequestID)
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Resume unblocks a paused request with a decision.
func (m *Manager) Resume(requestID string, decision *ResumeDecision) error {
	m.mu.Lock()
	entry, ok := m.pausedReqs[requestID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no paused request %q", requestID)
	}
	entry.decision = decision
	entry.State = StateResumed
	close(entry.done)
	return nil
}

// Subscribe returns a channel that receives PausedEntry values as requests are paused.
// The caller must call Unsubscribe when done.
func (m *Manager) Subscribe() chan *PausedEntry {
	ch := make(chan *PausedEntry, 16)
	m.mu.Lock()
	m.subscribers[ch] = struct{}{}
	m.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel. O(1) via map lookup.
func (m *Manager) Unsubscribe(ch chan *PausedEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subscribers[ch]; ok {
		delete(m.subscribers, ch)
		close(ch)
	}
}
