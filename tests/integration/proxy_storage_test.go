// Package integration_test contains tests that wire together two or three
// real components (proxy, engine, storage) without gRPC to verify the core
// data flow: proxy captures request → engine stores it → storage persists it.
package integration_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/storage"
)

// proxyStack bundles a proxy + engine + storage for integration testing.
type proxyStack struct {
	proxy     *proxy.HTTPProxy
	engine    *engine.Engine
	db        *storage.DB
	bpMgr     *breakpoints.Manager
	proxyURL  string
	proxyLis  net.Listener
	httpSrv   *http.Server
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// newProxyStack starts a real HTTP proxy on a random port wired to an
// in-memory engine and storage. Caller must call stop() to clean up.
func newProxyStack(t *testing.T) *proxyStack {
	t.Helper()

	// Open in-memory database.
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	// Create engine, breakpoints, and plugin runtime.
	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

	// Create TLS certificate authority for HTTPS interception.
	dir := t.TempDir()
	ca, err := proxy.NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		db.Close()
		t.Fatalf("NewCertAuthority: %v", err)
	}

	cfg := &config.Config{
		HTTPReadHeaderTimeout: 10,
		HTTPReadTimeout:       30,
		HTTPWriteTimeout:      120,
		HTTPIdleTimeout:       120,
		MaxBodySizeMB:         32,
	}

	// Wire up HTTP and TLS proxies.
	tlsP := proxy.NewTLSProxy(ca, eng, proxy.TransportOptions{}, cfg)
	tlsP.SetPlugins(rt)

	httpP := proxy.NewHTTPProxy("", tlsP, eng, proxy.TransportOptions{}, cfg)
	httpP.SetPlugins(rt)

	// Start HTTP proxy on a random OS-assigned port.
	proxyLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		t.Fatalf("net.Listen: %v", err)
	}

	proxyPort := proxyLis.Addr().(*net.TCPAddr).Port
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", proxyPort)

	httpSrv := &http.Server{Handler: httpP}
	go httpSrv.Serve(proxyLis) //nolint:errcheck

	// Give the proxy time to start and be ready to accept requests.
	time.Sleep(50 * time.Millisecond)

	return &proxyStack{
		proxy:    httpP,
		engine:   eng,
		db:       db,
		bpMgr:    bpMgr,
		proxyURL: proxyURL,
		proxyLis: proxyLis,
		httpSrv:  httpSrv,
		stopCh:   make(chan struct{}),
	}
}

// stop closes all resources gracefully.
func (s *proxyStack) stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		_ = s.httpSrv.Close()
		_ = s.proxyLis.Close()
		_ = s.db.Close()
	})
}

// proxyHTTPClient returns an *http.Client configured to route requests through
// the proxy stack.
func (s *proxyStack) client() *http.Client {
	u, _ := url.Parse(s.proxyURL)
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   15 * time.Second,
	}
}

// ── Test 1: Proxy stores correct URL, method, status code, and body ────────

func TestIntegration_ProxyStoresCorrectFields(t *testing.T) {
	t.Parallel()

	stack := newProxyStack(t)
	defer stack.stop()

	// Mock upstream returns a fixed response.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "stored response body")
	}))
	defer upstream.Close()

	// Send a GET request with a custom header through the proxy.
	client := stack.client()
	req, err := http.NewRequest("GET", upstream.URL+"/test/path?key=value", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Custom-Header", "custom-value")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("response status: got %d want %d", resp.StatusCode, http.StatusCreated)
	}
	if got := string(body); got != "stored response body" {
		t.Errorf("response body: got %q want %q", got, "stored response body")
	}

	// Give the engine time to store the transaction.
	time.Sleep(100 * time.Millisecond)

	// Verify the transaction is in storage with correct fields.
	records, _, err := stack.db.ListTransactions(10, 0, "", "", 0)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least 1 transaction in storage, got 0")
	}

	txn := records[0]

	// Verify URL contains the path and query string.
	if !strings.Contains(txn.URL, "/test/path") {
		t.Errorf("stored URL missing path: got %q", txn.URL)
	}
	if !strings.Contains(txn.URL, "key=value") {
		t.Errorf("stored URL missing query: got %q", txn.URL)
	}

	// Verify method.
	if txn.Method != "GET" {
		t.Errorf("stored method: got %q want GET", txn.Method)
	}

	// Verify request headers were captured.
	headerStr := fmt.Sprintf("%v", txn.Headers)
	if !strings.Contains(headerStr, "X-Custom-Header") {
		t.Errorf("stored headers missing X-Custom-Header: got %v", txn.Headers)
	}

	// Verify status code in response.
	_, respRec, err := stack.db.GetTransaction(txn.ID)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if respRec.StatusCode != http.StatusCreated {
		t.Errorf("stored response status: got %d want %d", respRec.StatusCode, http.StatusCreated)
	}
	if !strings.Contains(string(respRec.Body), "stored response body") {
		t.Errorf("stored response body: got %q", string(respRec.Body))
	}
}

// ── Test 2: Proxy correctly stores various status codes ────────────────────

func TestIntegration_ProxyPreservesStatusCodes(t *testing.T) {
	t.Parallel()

	stack := newProxyStack(t)
	defer stack.stop()

	cases := []int{http.StatusOK, http.StatusCreated, http.StatusNotFound, http.StatusInternalServerError}

	for _, wantStatus := range cases {
		wantStatus := wantStatus

		t.Run(fmt.Sprintf("Status_%d", wantStatus), func(t *testing.T) {
			// Mock upstream that returns a specific status code.
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(wantStatus)
			}))
			defer upstream.Close()

			// Send request through proxy.
			client := stack.client()
			resp, err := client.Get(upstream.URL + "/test")
			if err != nil {
				t.Fatalf("proxy GET: %v", err)
			}
			defer resp.Body.Close()

			// Give the engine time to store.
			time.Sleep(50 * time.Millisecond)

			// Read stored transaction from DB.
			records, _, err := stack.db.ListTransactions(100, 0, "", "", 0)
			if err != nil {
				t.Fatalf("ListTransactions: %v", err)
			}

			// Find the most recent transaction for this status code.
			var found *storage.RequestRecord
			for _, rec := range records {
				if strings.Contains(rec.URL, "/test") {
					found = rec
					break
				}
			}
			if found == nil {
				t.Fatal("transaction not found in storage")
			}

			// Verify the stored status code matches.
			_, respRec, err := stack.db.GetTransaction(found.ID)
			if err != nil {
				t.Fatalf("GetTransaction: %v", err)
			}
			if respRec.StatusCode != wantStatus {
				t.Errorf("stored status: got %d want %d", respRec.StatusCode, wantStatus)
			}
		})
	}
}

// ── Test 3: Concurrent proxy requests all stored without duplicates ────────

func TestIntegration_ConcurrentProxyWrites(t *testing.T) {
	t.Parallel()

	stack := newProxyStack(t)
	defer stack.stop()

	// Mock upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)

	var successCount atomic.Int32

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			client := stack.client()
			resp, err := client.Get(fmt.Sprintf("%s/concurrent/%d", upstream.URL, idx))
			if err != nil {
				t.Logf("request %d failed: %v", idx, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if successCount.Load() < int32(n) {
		t.Logf("not all requests succeeded: %d/%d", successCount.Load(), n)
	}

	// Give the engine time to process all transactions.
	time.Sleep(200 * time.Millisecond)

	// Verify all requests were stored.
	records, _, err := stack.db.ListTransactions(int(n*2), 0, "", "", 0)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}

	if len(records) < n {
		t.Errorf("expected at least %d stored transactions after %d concurrent requests, got %d", n, n, len(records))
	}

	// Verify no duplicates by checking unique IDs.
	idSet := make(map[string]bool)
	for _, rec := range records {
		if idSet[rec.ID] {
			t.Errorf("duplicate transaction ID: %s", rec.ID)
		}
		idSet[rec.ID] = true
	}
}

// ── Test 4: Engine correctly updates stored request when modified ──────────

func TestIntegration_ModifiedRequestStored(t *testing.T) {
	t.Parallel()

	stack := newProxyStack(t)
	defer stack.stop()

	// Mock upstream echoes back the request path.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Original-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Add a breakpoint that triggers on /modify.
	_, err := stack.bpMgr.AddRule(&breakpoints.BreakpointRule{
		URLPattern: `.*modify.*`,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Goroutine to handle paused requests and modify them.
	pausedCh := stack.bpMgr.Subscribe()
	go func() {
		for entry := range pausedCh {
			// Modify the request path.
			modURL, _ := url.Parse(upstream.URL + "/modified")
			entry.Request.URL = modURL
			_ = stack.bpMgr.Resume(entry.RequestID, &breakpoints.ResumeDecision{
				Action:            breakpoints.ActionForward,
				ModifiedRequest:   entry.Request,
			})
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Send request through proxy to the breakpoint.
	client := stack.client()
	resp, err := client.Get(upstream.URL + "/modify/me")
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	// Give engine time to store.
	time.Sleep(100 * time.Millisecond)

	// Verify the stored request shows the modified path.
	records, _, err := stack.db.ListTransactions(10, 0, "", "", 0)
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}

	if len(records) == 0 {
		t.Fatal("no transactions stored")
	}

	// After modification and forward, the stored URL reflects the modified target.
	if !strings.Contains(records[0].URL, "/modified") {
		t.Errorf("stored URL should show modified path: got %q", records[0].URL)
	}
}
