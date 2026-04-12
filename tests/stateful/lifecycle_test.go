package stateful_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func newTestStack(t *testing.T) (*engine.Engine, *breakpoints.Manager, *storage.DB) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)
	return eng, bpMgr, db
}

func makeTx(id, method, rawURL string) *proxy.Transaction {
	req, _ := http.NewRequest(method, rawURL, nil)
	return &proxy.Transaction{
		ID: id,
		Request: &plugins.ProxyRequest{
			ID: id, Method: req.Method, URL: req.URL,
			Headers: req.Header, Raw: req,
		},
	}
}

// pauseAsync runs bpMgr.Pause in a goroutine and returns a channel that
// receives the ResumeDecision when Pause returns.
func pauseAsync(t *testing.T, bpMgr *breakpoints.Manager, entry *breakpoints.PausedEntry) <-chan *breakpoints.ResumeDecision {
	t.Helper()
	ch := make(chan *breakpoints.ResumeDecision, 1)
	go func() {
		dec, err := bpMgr.Pause(context.Background(), entry)
		if err != nil {
			t.Errorf("Pause returned error: %v", err)
		}
		ch <- dec
	}()
	return ch
}

// waitDecision blocks until pauseAsync returns a decision or the test times out.
func waitDecision(t *testing.T, ch <-chan *breakpoints.ResumeDecision) *breakpoints.ResumeDecision {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Pause to return a decision")
		return nil
	}
}

// waitPaused blocks until a paused entry arrives on sub or the test times out.
func waitPaused(t *testing.T, sub <-chan *breakpoints.PausedEntry) *breakpoints.PausedEntry {
	t.Helper()
	select {
	case e := <-sub:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for paused entry on subscriber channel")
		return nil
	}
}

// ---------------------------------------------------------------------------
// ActionForward
// ---------------------------------------------------------------------------

func TestStateful_ForwardAction(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	rule, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: ".*", Enabled: true})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/forward", nil)
	entry := breakpoints.NewPausedEntry("tx-forward", rule.ID, req)

	sub := bpMgr.Subscribe()
	defer bpMgr.Unsubscribe(sub)

	decCh := pauseAsync(t, bpMgr, entry)
	e := waitPaused(t, sub)

	if err := bpMgr.Resume(e.RequestID, &breakpoints.ResumeDecision{Action: breakpoints.ActionForward}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	dec := waitDecision(t, decCh)
	if dec.Action != breakpoints.ActionForward {
		t.Fatalf("expected ActionForward, got %v", dec.Action)
	}
}

// ---------------------------------------------------------------------------
// ActionDrop
// ---------------------------------------------------------------------------

func TestStateful_DropAction(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	rule, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: "drop", Enabled: true})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	req, _ := http.NewRequest(http.MethodDelete, "http://drop.example.com/", nil)
	entry := breakpoints.NewPausedEntry("tx-drop", rule.ID, req)

	sub := bpMgr.Subscribe()
	defer bpMgr.Unsubscribe(sub)

	decCh := pauseAsync(t, bpMgr, entry)
	e := waitPaused(t, sub)

	if err := bpMgr.Resume(e.RequestID, &breakpoints.ResumeDecision{Action: breakpoints.ActionDrop}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	dec := waitDecision(t, decCh)
	if dec.Action != breakpoints.ActionDrop {
		t.Fatalf("expected ActionDrop, got %v", dec.Action)
	}
}

// ---------------------------------------------------------------------------
// Invalid transition: resume non-existent request
// ---------------------------------------------------------------------------

func TestStateful_InvalidTransition_ResumeNonExistent(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	err := bpMgr.Resume("does-not-exist", &breakpoints.ResumeDecision{Action: breakpoints.ActionForward})
	if err == nil {
		t.Fatal("expected error when resuming non-existent request")
	}
}

// ---------------------------------------------------------------------------
// Invalid transition: double-resume the same request
// ---------------------------------------------------------------------------

func TestStateful_InvalidTransition_DoubleResume(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	rule, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: ".*", Enabled: true})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	entry := breakpoints.NewPausedEntry("tx-double", rule.ID, req)

	sub := bpMgr.Subscribe()
	defer bpMgr.Unsubscribe(sub)

	decCh := pauseAsync(t, bpMgr, entry)
	e := waitPaused(t, sub)

	// First resume — must succeed.
	if err := bpMgr.Resume(e.RequestID, &breakpoints.ResumeDecision{Action: breakpoints.ActionForward}); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	waitDecision(t, decCh) // drain so Pause completes and removes the entry

	// Second resume — must fail (entry was cleaned up).
	if err := bpMgr.Resume(e.RequestID, &breakpoints.ResumeDecision{Action: breakpoints.ActionDrop}); err == nil {
		t.Fatal("expected second resume to fail")
	}
}

// ---------------------------------------------------------------------------
// Replay from stored history
// ---------------------------------------------------------------------------

func TestStateful_ReplayFromHistory(t *testing.T) {
	_, _, db := newTestStack(t)

	// Start a local echo server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		for k, vv := range r.Header {
			w.Write([]byte(k + ":" + vv[0] + "\n"))
		}
	}))
	defer srv.Close()

	// Seed a stored request record pointing at the test server.
	rec := &storage.RequestRecord{
		ID:     "replay-hist-1",
		Method: "GET",
		URL:    srv.URL + "/api",
		Headers: map[string]string{
			"User-Agent":     "apix-test/1.0",
			"X-Replay-Header": "present",
		},
	}
	if err := db.SaveRequest(rec); err != nil {
		t.Fatalf("save request: %v", err)
	}

	re := replay.NewEngine(db, nil)

	// Replay using just the stored ID — engine loads URL from storage.
	resp, err := re.ReplayRequest(context.Background(), &replay.ReplayRequest{
		RequestID: "replay-hist-1",
	})
	if err != nil {
		t.Fatalf("replay Execute: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("X-Replay-Header")) {
		t.Fatalf("expected X-Replay-Header echoed in body, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Replay with body mutation
// ---------------------------------------------------------------------------

func TestStateful_ReplayWithBodyMutation(t *testing.T) {
	_, _, db := newTestStack(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write(b) // echo body back
	}))
	defer srv.Close()

	if err := db.SaveRequest(&storage.RequestRecord{
		ID:     "replay-body-1",
		Method: "POST",
		URL:    srv.URL + "/post",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: []byte(`{"original": true}`),
	}); err != nil {
		t.Fatalf("save request: %v", err)
	}

	re := replay.NewEngine(db, nil)

	newBody := []byte(`{"mutated": true}`)
	resp, err := re.ReplayRequest(context.Background(), &replay.ReplayRequest{
		RequestID:    "replay-body-1",
		OverrideBody: newBody,
	})
	if err != nil {
		t.Fatalf("replay Execute: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("mutated")) {
		t.Fatalf("expected mutated body echoed, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Concurrent subscribers — all receive the paused entry broadcast
// ---------------------------------------------------------------------------

func TestStateful_ConcurrentSubscribers(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	rule, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: ".*", Enabled: true})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	const numSubs = 5
	subs := make([]chan *breakpoints.PausedEntry, numSubs)
	for i := range subs {
		subs[i] = bpMgr.Subscribe()
	}
	t.Cleanup(func() {
		for _, s := range subs {
			bpMgr.Unsubscribe(s)
		}
	})

	req, _ := http.NewRequest(http.MethodGet, "http://broadcast.example.com/", nil)
	entry := breakpoints.NewPausedEntry("tx-broadcast", rule.ID, req)

	pauseDecCh := pauseAsync(t, bpMgr, entry)

	// All subscribers must receive the notification.
	var wg sync.WaitGroup
	wg.Add(numSubs)
	for _, s := range subs {
		s := s
		go func() {
			defer wg.Done()
			select {
			case <-s:
			case <-time.After(2 * time.Second):
				t.Errorf("subscriber timed out")
			}
		}()
	}
	wg.Wait()

	// Resume once — only one actor should win; others see a clean error.
	if err := bpMgr.Resume("tx-broadcast", &breakpoints.ResumeDecision{Action: breakpoints.ActionForward}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitDecision(t, pauseDecCh)
}

// ---------------------------------------------------------------------------
// Disabled rule: must not fire
// ---------------------------------------------------------------------------

func TestStateful_DisabledRuleDoesNotMatch(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	// Register a disabled rule.
	_, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: ".*", Enabled: false})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	matched := bpMgr.Evaluate("GET", "http://example.com/")
	if matched != "" {
		t.Fatalf("disabled rule should not match, got rule ID %q", matched)
	}
}

// ---------------------------------------------------------------------------
// Rule removal: must clean up matching state
// ---------------------------------------------------------------------------

func TestStateful_RuleRemovalPreventsMatch(t *testing.T) {
	_, bpMgr, _ := newTestStack(t)

	rule, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: "cleanup-target", Enabled: true})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	if bpMgr.Evaluate("GET", "http://cleanup-target.example.com/") == "" {
		t.Fatal("expected rule to match before removal")
	}

	if err := bpMgr.RemoveRule(rule.ID); err != nil {
		t.Fatalf("remove rule: %v", err)
	}

	if matched := bpMgr.Evaluate("GET", "http://cleanup-target.example.com/"); matched != "" {
		t.Fatalf("expected no match after rule removal, got %q", matched)
	}
}

// ---------------------------------------------------------------------------
// Full lifecycle: engine.PauseRequest + Respond + StoreTransaction + DB read
// ---------------------------------------------------------------------------

func TestStateful_FullLifecycle_EngineLevel(t *testing.T) {
	eng, bpMgr, db := newTestStack(t)

	if _, err := bpMgr.AddRule(&breakpoints.BreakpointRule{URLPattern: ".*", Enabled: true}); err != nil {
		t.Fatalf("add rule: %v", err)
	}

	tx := makeTx("tx-lifecycle", "GET", "http://example.com/lifecycle")

	sub := bpMgr.Subscribe()
	defer bpMgr.Unsubscribe(sub)

	errCh := make(chan error, 1)
	go func() {
		_, _, err := eng.PauseRequest(tx)
		errCh <- err
	}()

	e := waitPaused(t, sub)

	synthResp := &http.Response{StatusCode: 418, Status: "418 I'm a teapot", Body: http.NoBody}
	if err := bpMgr.Resume(e.RequestID, &breakpoints.ResumeDecision{
		Action:           breakpoints.ActionRespond,
		ModifiedResponse: synthResp,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("PauseRequest error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PauseRequest did not return")
	}

	if err := eng.StoreTransaction(tx); err != nil {
		t.Fatalf("store transaction: %v", err)
	}

	reqRec, _, err := db.GetTransaction(tx.ID)
	if err != nil {
		t.Fatalf("get transaction: %v", err)
	}
	if reqRec == nil {
		t.Fatal("expected stored request record")
	}
	if reqRec.URL != "http://example.com/lifecycle" {
		t.Fatalf("unexpected URL %q", reqRec.URL)
	}
}
