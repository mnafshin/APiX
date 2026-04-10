package breakpoints

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestAddRule(t *testing.T) {
	t.Parallel()
	m := NewManager()
	rule := &BreakpointRule{
		URLPattern: ".*example\\.com.*",
		Methods:    []string{"GET"},
		Enabled:    true,
		Label:      "test rule",
	}
	added, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if added.ID == "" {
		t.Error("expected ID to be assigned")
	}

	rules := m.ListRules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].ID != added.ID {
		t.Errorf("ID mismatch: got %q want %q", rules[0].ID, added.ID)
	}
}

func TestAddRuleEmptyPattern(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{URLPattern: "", Enabled: true})
	if err == nil {
		t.Error("expected error for empty url_pattern, got nil")
	}
}

func TestAddRulePatternTooLong(t *testing.T) {
	t.Parallel()
	m := NewManager()
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	_, err := m.AddRule(&BreakpointRule{URLPattern: string(long), Enabled: true})
	if err == nil {
		t.Error("expected error for pattern > 500 chars, got nil")
	}
}

func TestAddRuleInvalidMethod(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{
		URLPattern: ".*",
		Methods:    []string{"INVALID"},
		Enabled:    true,
	})
	if err == nil {
		t.Error("expected error for invalid HTTP method, got nil")
	}
}

func TestAddRuleValidMethods(t *testing.T) {
	t.Parallel()
	m := NewManager()
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE"}
	for _, method := range methods {
		_, err := m.AddRule(&BreakpointRule{
			URLPattern: ".*",
			Methods:    []string{method},
			Enabled:    true,
		})
		if err != nil {
			t.Errorf("AddRule with method %q: unexpected error: %v", method, err)
		}
	}
}

func TestAddRuleInvalidRegex(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{
		URLPattern: "[invalid",
		Enabled:    true,
	})
	if err == nil {
		t.Error("expected error for invalid regex, got nil")
	}
}

func TestRemoveRule(t *testing.T) {
	t.Parallel()
	m := NewManager()
	rule, err := m.AddRule(&BreakpointRule{URLPattern: ".*", Enabled: true})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	if err := m.RemoveRule(rule.ID); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}

	rules := m.ListRules()
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after removal, got %d", len(rules))
	}
}

func TestEvaluateMatch(t *testing.T) {
	t.Parallel()
	m := NewManager()
	rule, err := m.AddRule(&BreakpointRule{
		URLPattern: `.*example\.com.*`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	id := m.Evaluate("GET", "https://example.com/path")
	if id != rule.ID {
		t.Errorf("Evaluate: got %q want %q", id, rule.ID)
	}
}

func TestEvaluateNoMatch(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{
		URLPattern: `.*example\.com.*`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	id := m.Evaluate("GET", "https://other.com")
	if id != "" {
		t.Errorf("expected no match, got %q", id)
	}
}

func TestEvaluateMethodFilter(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{
		URLPattern: ".*",
		Methods:    []string{"POST"},
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	if id := m.Evaluate("GET", "https://example.com"); id != "" {
		t.Errorf("GET should not match POST-only rule, got %q", id)
	}
	if id := m.Evaluate("POST", "https://example.com"); id == "" {
		t.Error("POST should match POST-only rule")
	}
}

func TestEvaluateDisabledRule(t *testing.T) {
	t.Parallel()
	m := NewManager()
	_, err := m.AddRule(&BreakpointRule{
		URLPattern: ".*",
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	id := m.Evaluate("GET", "https://example.com")
	if id != "" {
		t.Errorf("disabled rule should not match, got %q", id)
	}
}

func TestPauseAndResume(t *testing.T) {
	t.Parallel()
	m := NewManager()

	entry := NewPausedEntry("req-pause-resume", "bp-1", &http.Request{})

	decision := &ResumeDecision{Action: ActionForward}

	// Resume after 100ms.
	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := m.Resume("req-pause-resume", decision); err != nil {
			t.Errorf("Resume: %v", err)
		}
	}()

	got, err := m.Pause(context.Background(), entry)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if got == nil {
		t.Fatal("expected decision, got nil")
	}
	if got.Action != ActionForward {
		t.Errorf("Action: got %d want %d", got.Action, ActionForward)
	}
}

func TestPauseContextCancel(t *testing.T) {
	t.Parallel()
	m := NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	entry := NewPausedEntry("req-ctx-cancel", "bp-1", &http.Request{})

	cancel() // cancel immediately

	_, err := m.Pause(ctx, entry)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestSubscribeReceivesPausedEntry(t *testing.T) {
	t.Parallel()
	m := NewManager()

	ch := m.Subscribe()
	defer m.Unsubscribe(ch)

	entry := NewPausedEntry("req-subscribe", "bp-sub", &http.Request{})

	// Pause in a goroutine.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Provide a decision so Pause eventually returns.
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = m.Resume("req-subscribe", &ResumeDecision{Action: ActionForward})
		}()
		m.Pause(ctx, entry) //nolint:errcheck
	}()

	select {
	case got := <-ch:
		if got == nil {
			t.Error("received nil entry")
		}
		if got.RequestID != "req-subscribe" {
			t.Errorf("RequestID: got %q", got.RequestID)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for paused entry on subscriber channel")
	}
}

// ── ReDoS timeout ──────────────────────────────────────────────────────────

// TestEvaluate_ReDoS_Timeout checks that a catastrophic backtracking regex
// does not block the caller for longer than ~300ms.
func TestEvaluate_ReDoS_Timeout(t *testing.T) {
	t.Parallel()
	m := NewManager()

	// This pattern can trigger exponential backtracking with some engines.
	// Go's regexp package uses a linear-time algorithm, so the risk here is
	// the goroutine timeout added in manager.go being exercised properly.
	if _, err := m.AddRule(&BreakpointRule{
		URLPattern: `(https?://)?([a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}(/[a-zA-Z0-9-_%]+)+`,
		Enabled:    true,
		Label:      "complex",
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Even with a complex pattern, Evaluate should return within the timeout.
	start := time.Now()
	_ = m.Evaluate("GET", "https://this.is.a.very.long.host.example.com/path/to/resource/item")
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("Evaluate took %v, want <500ms", elapsed)
	}
}

// ── Multiple rules – first match ───────────────────────────────────────────

func TestEvaluate_MultipleRules_FirstMatch(t *testing.T) {
	t.Parallel()
	m := NewManager()

	rule1, _ := m.AddRule(&BreakpointRule{URLPattern: `.*users.*`, Enabled: true, Label: "users"})
	rule2, _ := m.AddRule(&BreakpointRule{URLPattern: `.*example.*`, Enabled: true, Label: "example"})

	// Both patterns match this URL; exactly one rule ID should be returned.
	matched := m.Evaluate("GET", "https://api.example.com/users/123")
	if matched == "" {
		t.Fatal("expected a match, got empty string")
	}
	if matched != rule1.ID && matched != rule2.ID {
		t.Errorf("matched ID %q is not one of the known rule IDs (%q, %q)", matched, rule1.ID, rule2.ID)
	}
}

// ── Resume unknown ID ──────────────────────────────────────────────────────

func TestResume_UnknownID(t *testing.T) {
	t.Parallel()
	m := NewManager()

	err := m.Resume("nonexistent-request-id", &ResumeDecision{Action: ActionForward})
	if err == nil {
		t.Error("expected error for unknown request ID, got nil")
	}
}

// ---- Fuzz tests ----

// FuzzEvaluate ensures Manager.Evaluate never panics regardless of the method
// and rawURL values it receives. The implementation already guards against
// ReDoS via a timeout, so this fuzz test focuses on correctness under
// adversarial input: control characters, binary data, malformed URLs, and
// extremely long strings.
func FuzzEvaluate(f *testing.F) {
	f.Add("GET", "https://example.com/api/users")
	f.Add("POST", "http://localhost:8080/[invalid")     // malformed URL
	f.Add("", "")                                        // empty inputs
	f.Add("DELETE", "https://api.example.com/\x00path") // null byte
	f.Add("OPTIONS", "not-a-url-at-all")
	f.Add("GET", "https://"+string(make([]byte, 1024))) // long URL

	m := NewManager()
	if _, err := m.AddRule(&BreakpointRule{
		URLPattern: `.*api.*`,
		Enabled:    true,
	}); err != nil {
		f.Fatal("AddRule:", err)
	}
	if _, err := m.AddRule(&BreakpointRule{
		URLPattern: `.*`,
		Methods:    []string{"POST"},
		Enabled:    true,
	}); err != nil {
		f.Fatal("AddRule:", err)
	}

	f.Fuzz(func(t *testing.T, method, rawURL string) {
		// Must not panic regardless of input; return value may be "" or a rule ID.
		_ = m.Evaluate(method, rawURL)
	})
}
