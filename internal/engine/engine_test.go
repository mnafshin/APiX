package engine_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
)

// newTestEngine opens an in-memory DB and wires a fresh engine.
func newTestEngine(t *testing.T) *engine.Engine {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	e := engine.New(db, bpMgr, rt)
	return e
}

// makeTransaction builds a minimal proxy.Transaction with a real *http.Request.
func makeTransaction(id, method, rawURL string) *proxy.Transaction {
	u, _ := url.Parse(rawURL)
	raw, _ := http.NewRequest(method, rawURL, nil)
	return &proxy.Transaction{
		ID: id,
		Request: &proxy.ProxyRequest{
			ID:      id,
			Method:  method,
			URL:     u,
			Headers: http.Header{"X-Test": []string{"hello"}},
			Raw:     raw,
		},
	}
}

// TestStoreTransaction verifies that a stored transaction is retrievable from the DB.
func TestStoreTransaction(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	tx := makeTransaction("tx-store-1", "GET", "http://example.com/foo")
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	req, _, err := e.DB().GetTransaction("tx-store-1")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if req == nil {
		t.Fatal("expected request record, got nil")
	}
	if req.Method != "GET" {
		t.Errorf("method: got %q want %q", req.Method, "GET")
	}
	if req.URL != "http://example.com/foo" {
		t.Errorf("url: got %q want %q", req.URL, "http://example.com/foo")
	}
}

// TestStoreTransactionNilIsNoop verifies that storing nil does nothing.
func TestStoreTransactionNilIsNoop(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)
	if err := e.StoreTransaction(nil); err != nil {
		t.Fatalf("expected nil error for nil tx, got %v", err)
	}
}

// TestStoreTransactionAssignsID verifies that a missing ID gets auto-assigned.
func TestStoreTransactionAssignsID(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	u, _ := url.Parse("http://example.com/bar")
	raw, _ := http.NewRequest("POST", "http://example.com/bar", nil)
	tx := &proxy.Transaction{
		// ID deliberately empty
		Request: &proxy.ProxyRequest{
			Method:  "POST",
			URL:     u,
			Headers: http.Header{},
			Raw:     raw,
		},
	}
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}
	if tx.ID == "" {
		t.Error("expected ID to be auto-assigned")
	}
	req, _, err := e.DB().GetTransaction(tx.ID)
	if err != nil || req == nil {
		t.Fatalf("GetTransaction(%q): req=%v err=%v", tx.ID, req, err)
	}
}

// TestStoreTransactionWithResponse verifies that response data is also persisted.
func TestStoreTransactionWithResponse(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	tx := makeTransaction("tx-with-resp", "GET", "http://example.com/resp")
	tx.Response = &proxy.ProxyResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	_, resp, err := e.DB().GetTransaction("tx-with-resp")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response record, got nil")
	}
	if resp.StatusCode != 200 {
		t.Errorf("status code: got %d want 200", resp.StatusCode)
	}
}

// TestSubscribeReceivesTransaction verifies that a subscriber gets notified.
func TestSubscribeReceivesTransaction(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	ch := e.Subscribe()
	t.Cleanup(func() { e.Unsubscribe(ch) })

	tx := makeTransaction("tx-sub-1", "GET", "http://example.com/sub")
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	select {
	case msg := <-ch:
		if msg.Id != "tx-sub-1" {
			t.Errorf("got id %q want %q", msg.Id, "tx-sub-1")
		}
		if msg.Method != "GET" {
			t.Errorf("got method %q want %q", msg.Method, "GET")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscriber notification")
	}
}

// TestUnsubscribeStopsDelivery verifies that after Unsubscribe no messages arrive.
func TestUnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	ch := e.Subscribe()
	e.Unsubscribe(ch) // unsubscribe immediately

	tx := makeTransaction("tx-unsub-1", "PUT", "http://example.com/unsub")
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	select {
	case msg, ok := <-ch:
		if ok {
			t.Errorf("unexpected message after unsubscribe: %v", msg)
		}
		// channel closed — correct behaviour
	case <-time.After(100 * time.Millisecond):
		// nothing arrived — also correct
	}
}

// TestMultipleSubscribers verifies all subscribers receive the same event.
func TestMultipleSubscribers(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	ch1 := e.Subscribe()
	ch2 := e.Subscribe()
	t.Cleanup(func() {
		e.Unsubscribe(ch1)
		e.Unsubscribe(ch2)
	})

	tx := makeTransaction("tx-multi-sub", "DELETE", "http://example.com/multi")
	if err := e.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}

	select {
	case msg := <-ch1:
		if msg.Id != "tx-multi-sub" {
			t.Errorf("ch1: got id %q want %q", msg.Id, "tx-multi-sub")
		}
	case <-time.After(2 * time.Second):
		t.Error("ch1: timed out")
	}
	select {
	case msg := <-ch2:
		if msg.Id != "tx-multi-sub" {
			t.Errorf("ch2: got id %q want %q", msg.Id, "tx-multi-sub")
		}
	case <-time.After(2 * time.Second):
		t.Error("ch2: timed out")
	}
}

// TestPauseRequestForwardResume verifies the pause/resume flow with ActionForward.
func TestPauseRequestForwardResume(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	// Register a breakpoint rule.
	_, err := e.BreakpointManager().AddRule(&breakpoints.BreakpointRule{
		URLPattern: ".*example\\.com.*",
		Methods:    []string{"GET"},
		Enabled:    true,
		Label:      "test-bp",
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	tx := makeTransaction("tx-pause-1", "GET", "http://example.com/pause")

	// Subscribe to the breakpoint manager so we can detect when it's paused
	// and then resume it.
	bpCh := e.BreakpointManager().Subscribe()
	t.Cleanup(func() { e.BreakpointManager().Unsubscribe(bpCh) })

	// Run PauseRequest in a goroutine since it blocks until resumed.
	type result struct {
		tx     *proxy.Transaction
		action proxy.ResumeAction
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		outTx, action, err := e.PauseRequest(tx)
		resultCh <- result{outTx, action, err}
	}()

	// Wait for the paused notification, then resume.
	select {
	case entry := <-bpCh:
		if err := e.BreakpointManager().Resume(entry.RequestID, &breakpoints.ResumeDecision{
			Action: breakpoints.ActionForward,
		}); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for paused notification")
	}

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("PauseRequest: %v", r.err)
		}
		if r.action != proxy.ResumeForward {
			t.Errorf("action: got %v want ResumeForward", r.action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PauseRequest result")
	}
}

// TestPauseRequestDropResume verifies that ActionDrop is returned correctly.
func TestPauseRequestDropResume(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	_, err := e.BreakpointManager().AddRule(&breakpoints.BreakpointRule{
		URLPattern: ".*drop\\.test.*",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	tx := makeTransaction("tx-drop-1", "POST", "http://drop.test/endpoint")

	bpCh := e.BreakpointManager().Subscribe()
	t.Cleanup(func() { e.BreakpointManager().Unsubscribe(bpCh) })

	type result struct {
		tx     *proxy.Transaction
		action proxy.ResumeAction
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		outTx, action, err := e.PauseRequest(tx)
		resultCh <- result{outTx, action, err}
	}()

	select {
	case entry := <-bpCh:
		if err := e.BreakpointManager().Resume(entry.RequestID, &breakpoints.ResumeDecision{
			Action: breakpoints.ActionDrop,
		}); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for paused notification")
	}

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("PauseRequest: %v", r.err)
		}
		if r.action != proxy.ResumeDrop {
			t.Errorf("action: got %v want ResumeDrop", r.action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PauseRequest result")
	}
}

// TestPauseRequestRespondAction verifies that ActionRespond populates tx.Response.
func TestPauseRequestRespondAction(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	_, err := e.BreakpointManager().AddRule(&breakpoints.BreakpointRule{
		URLPattern: ".*respond\\.test.*",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	tx := makeTransaction("tx-respond-1", "GET", "http://respond.test/path")

	bpCh := e.BreakpointManager().Subscribe()
	t.Cleanup(func() { e.BreakpointManager().Unsubscribe(bpCh) })

	type result struct {
		tx     *proxy.Transaction
		action proxy.ResumeAction
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		outTx, action, err := e.PauseRequest(tx)
		resultCh <- result{outTx, action, err}
	}()

	syntheticResp := &http.Response{
		StatusCode: 403,
		Status:     "403 Forbidden",
		Header:     http.Header{"X-Synthetic": []string{"true"}},
		Body:       io.NopCloser(strings.NewReader("forbidden")),
	}

	select {
	case entry := <-bpCh:
		if err := e.BreakpointManager().Resume(entry.RequestID, &breakpoints.ResumeDecision{
			Action:           breakpoints.ActionRespond,
			ModifiedResponse: syntheticResp,
		}); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for paused notification")
	}

	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("PauseRequest: %v", r.err)
		}
		if r.action != proxy.ResumeRespond {
			t.Errorf("action: got %v want ResumeRespond", r.action)
		}
		if r.tx.Response == nil {
			t.Fatal("expected synthetic response to be populated")
		}
		if r.tx.Response.StatusCode != 403 {
			t.Errorf("status code: got %d want 403", r.tx.Response.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PauseRequest result")
	}
}

// TestPauseRequestContextCancelled verifies that context cancellation unblocks Pause.
func TestPauseRequestContextCancelled(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	_, err := e.BreakpointManager().AddRule(&breakpoints.BreakpointRule{
		URLPattern: ".*cancel\\.test.*",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	tx := makeTransaction("tx-cancel-1", "GET", "http://cancel.test/path")
	tx.Request.Raw = tx.Request.Raw.WithContext(ctx)

	bpCh := e.BreakpointManager().Subscribe()
	t.Cleanup(func() { e.BreakpointManager().Unsubscribe(bpCh) })

	type result struct {
		action proxy.ResumeAction
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		_, action, err := e.PauseRequest(tx)
		resultCh <- result{action, err}
	}()

	// Wait for the request to be paused, then cancel the context.
	select {
	case <-bpCh:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for paused notification")
	}

	select {
	case r := <-resultCh:
		if r.err == nil {
			t.Error("expected error from context cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for PauseRequest to return after context cancel")
	}
}

// TestPauseRequestNilRequest verifies that a nil request passes through without error.
func TestPauseRequestNilRequest(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t)

	tx := &proxy.Transaction{ID: "tx-nil-req"}
	_, action, err := e.PauseRequest(tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != proxy.ResumeForward {
		t.Errorf("action: got %v want ResumeForward", action)
	}
}

// --- Plugin tests ---

// headerPlugin is a test plugin that injects a header into every request.
type headerPlugin struct {
	name  string
	key   string
	value string
}

func (p *headerPlugin) Name() string        { return p.name }
func (p *headerPlugin) Version() string     { return "0.0.1" }
func (p *headerPlugin) Description() string { return "test plugin" }
func (p *headerPlugin) OnRequest(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	clone := req.Clone(req.Body)
	clone.Headers.Set(p.key, p.value)
	return clone, nil
}
func (p *headerPlugin) OnResponse(_ context.Context, _ *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}

// responseHeaderPlugin injects a header into every response.
type responseHeaderPlugin struct {
	name  string
	key   string
	value string
}

func (p *responseHeaderPlugin) Name() string        { return p.name }
func (p *responseHeaderPlugin) Version() string     { return "0.0.1" }
func (p *responseHeaderPlugin) Description() string { return "response test plugin" }
func (p *responseHeaderPlugin) OnRequest(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	return nil, nil
}
func (p *responseHeaderPlugin) OnResponse(_ context.Context, _ *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	clone := resp.Clone(resp.Body)
	clone.Headers.Set(p.key, p.value)
	return clone, nil
}

// TestProcessRequestPluginModifiesHeader verifies that a plugin can add a header.
func TestProcessRequestPluginModifiesHeader(t *testing.T) {
	t.Parallel()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	rt := pluginrt.NewRuntime()
	if err := rt.Register(&headerPlugin{
		name:  "inject-header",
		key:   "X-Plugin-Header",
		value: "injected",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, _ := url.Parse("http://example.com/plugin-req")
	req := &plugins.ProxyRequest{
		ID:      "plugin-req-1",
		Method:  "GET",
		URL:     u,
		Headers: http.Header{},
		Body:    io.NopCloser(strings.NewReader("")),
	}

	modified, err := rt.RunRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("RunRequest: %v", err)
	}
	if got := modified.Headers.Get("X-Plugin-Header"); got != "injected" {
		t.Errorf("X-Plugin-Header: got %q want %q", got, "injected")
	}
}

// TestProcessResponsePluginModifiesHeader verifies plugin response header injection.
func TestProcessResponsePluginModifiesHeader(t *testing.T) {
	t.Parallel()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	rt := pluginrt.NewRuntime()
	if err := rt.Register(&responseHeaderPlugin{
		name:  "inject-resp-header",
		key:   "X-Response-Plugin",
		value: "resp-injected",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	u, _ := url.Parse("http://example.com/plugin-resp")
	req := &plugins.ProxyRequest{
		ID:     "plugin-resp-1",
		Method: "GET",
		URL:    u,
	}
	resp := &plugins.ProxyResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
	}

	modified, err := rt.RunResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("RunResponse: %v", err)
	}
	if got := modified.Headers.Get("X-Response-Plugin"); got != "resp-injected" {
		t.Errorf("X-Response-Plugin: got %q want %q", got, "resp-injected")
	}
}

// TestEngineAccessors verifies DB, BreakpointManager, PluginRuntime accessors.
func TestEngineAccessors(t *testing.T) {
	t.Parallel()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	e := engine.New(db, bpMgr, rt)

	if e.DB() != db {
		t.Error("DB() returned unexpected value")
	}
	if e.BreakpointManager() != bpMgr {
		t.Error("BreakpointManager() returned unexpected value")
	}
	if e.PluginRuntime() != rt {
		t.Error("PluginRuntime() returned unexpected value")
	}
}
