package breakpoints

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

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
	subscribers []chan *PausedEntry        // streams watching for paused requests
}

// NewManager creates an empty breakpoint manager.
func NewManager() *Manager {
	return &Manager{
		rules:      make(map[string]*BreakpointRule),
		compiled:   make(map[string]*regexp.Regexp),
		pausedReqs: make(map[string]*PausedEntry),
	}
}

// validHTTPMethods is the set of RFC 9110 standard methods.
var validHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

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
// or empty string if no rule matches.
func (m *Manager) Evaluate(method, rawURL string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, rule := range m.rules {
		if !rule.Enabled {
			continue
		}
		// Check method: empty methods list means match all.
		if len(rule.Methods) > 0 {
			matched := false
			for _, m := range rule.Methods {
				if strings.EqualFold(m, method) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		re := m.compiled[id]
		if re != nil && re.MatchString(rawURL) {
			return id
		}
	}
	return ""
}

// Pause holds req until resumed, broadcasting to all Watch subscribers.
// Blocks until ResumeRequest is called for requestID.
func (m *Manager) Pause(ctx context.Context, entry *PausedEntry) (*ResumeDecision, error) {
	m.mu.Lock()
	m.pausedReqs[entry.RequestID] = entry

	// Snapshot subscribers while holding the lock.
	subs := make([]chan *PausedEntry, len(m.subscribers))
	copy(subs, m.subscribers)
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
	m.subscribers = append(m.subscribers, ch)
	m.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (m *Manager) Unsubscribe(ch chan *PausedEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sub := range m.subscribers {
		if sub == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}
