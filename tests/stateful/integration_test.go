package stateful_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
)

// Integration test: in-process Engine with real Breakpoint manager. The test
// subscribes to paused entries, resumes with a synthetic response, and asserts
// the stored transaction contains the response.
func TestIntegrationBreakpointFlow(t *testing.T) {
	tmp := t.TempDir()
	dbPath := tmp + "/test.db"
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer st.Close()

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(st, bpMgr, rt)

	// Register a rule that matches any URL and enable it.
	rule := &breakpoints.BreakpointRule{URLPattern: ".*", Enabled: true}
	if _, err := bpMgr.AddRule(rule); err != nil {
		t.Fatalf("add rule: %v", err)
	}

	// Prepare a proxy.Transaction with a simple http.Request
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatalf("build http request: %v", err)
	}
	proxyReq := &plugins.ProxyRequest{ID: "tx1", Method: req.Method, URL: req.URL, Headers: req.Header, Body: nil, Raw: req}
	tx := &proxy.Transaction{ID: "tx1", Request: proxyReq}

	// Subscribe to paused entries so we can resume when PauseRequest blocks.
	sub := bpMgr.Subscribe()
	defer bpMgr.Unsubscribe(sub)

	// Call PauseRequest in a goroutine since it will block until resumed.
	resCh := make(chan struct{})
	var pauseErr error
	go func() {
		_, _, pauseErr = eng.PauseRequest(tx)
		close(resCh)
	}()

	// Wait for a paused entry to be broadcast, with timeout.
	select {
	case entry := <-sub:
		// Resume with a synthetic HTTP response.
		resp := &http.Response{StatusCode: 200, Status: "200 OK", Body: http.NoBody}
		dec := &breakpoints.ResumeDecision{Action: breakpoints.ActionRespond, ModifiedResponse: resp}
		if err := bpMgr.Resume(entry.RequestID, dec); err != nil {
			t.Fatalf("resume failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for paused entry")
	}

	// Wait for PauseRequest to return
	select {
	case <-resCh:
		if pauseErr != nil {
			t.Fatalf("PauseRequest error: %v", pauseErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for PauseRequest to return")
	}

	// In production the proxy calls StoreTransaction after completing the response
	// flow. Simulate that here so storage contains the response.
	if err := eng.StoreTransaction(tx); err != nil {
		t.Fatalf("store transaction: %v", err)
	}

	// After storing, the engine should have persisted the transaction with response.
	reqRec, respRec, err := eng.DB().GetTransaction(tx.ID)
	if err != nil {
		t.Fatalf("get transaction: %v", err)
	}
	if reqRec == nil {
		t.Fatalf("expected stored request")
	}
	if respRec == nil || respRec.StatusCode != 200 {
		t.Fatalf("expected stored response status 200, got %+v", respRec)
	}
}
