package proxy_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	replayengine "github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
)

// --- shared test helpers ---

// newTestStack wires a complete in-memory proxy stack (HTTPProxy + TLSProxy +
// Engine) ready for use in integration tests.
func newTestStack(t *testing.T) (*proxy.HTTPProxy, *proxy.TLSProxy, *engine.Engine, *breakpoints.Manager, *pluginrt.Runtime) {
	t.Helper()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

	dir := t.TempDir()
	ca, err := proxy.NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}

	cfg := &config.Config{
		HTTPReadHeaderTimeout: 10,
		HTTPReadTimeout:       30,
		HTTPWriteTimeout:      120,
		HTTPIdleTimeout:       120,
		MaxBodySizeMB:         32,
	}

	tlsP := proxy.NewTLSProxy(ca, eng, proxy.TransportOptions{}, cfg)
	tlsP.SetPlugins(rt)

	httpP := proxy.NewHTTPProxy("", tlsP, eng, proxy.TransportOptions{}, cfg)
	httpP.SetPlugins(rt)

	return httpP, tlsP, eng, bpMgr, rt
}

// startAutoResume subscribes to bpMgr and immediately resumes every paused
// request with ActionForward, allowing tests to exercise the proxy without
// manually managing breakpoint lifecycle.
func startAutoResume(t *testing.T, bpMgr *breakpoints.Manager) {
	t.Helper()
	ch := bpMgr.Subscribe()
	go func() {
		for entry := range ch {
			_ = bpMgr.Resume(entry.RequestID, &breakpoints.ResumeDecision{
				Action: breakpoints.ActionForward,
			})
		}
	}()
	t.Cleanup(func() { bpMgr.Unsubscribe(ch) })
}

// testPlugin is a minimal plugins.Plugin whose OnRequest / OnResponse
// behaviour is provided via optional hook functions.
type testPlugin struct {
	name   string
	onReq  func(*plugins.ProxyRequest) *plugins.ProxyRequest
	onResp func(*plugins.ProxyResponse) *plugins.ProxyResponse
}

func (p *testPlugin) Name() string        { return p.name }
func (p *testPlugin) Version() string     { return "1.0.0" }
func (p *testPlugin) Description() string { return "test plugin" }

func (p *testPlugin) OnRequest(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if p.onReq != nil {
		return p.onReq(req), nil
	}
	return nil, nil
}

func (p *testPlugin) OnResponse(_ context.Context, _ *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if p.onResp != nil {
		return p.onResp(resp), nil
	}
	return nil, nil
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q: %v", raw, err)
	}
	return u
}

// --- tests ---

// TestHTTPProxy_BasicRequest verifies that a plain HTTP GET reaches the
// upstream server and that the response and stored transaction are correct.
func TestHTTPProxy_BasicRequest(t *testing.T) {
	t.Parallel()

	const wantBody = "hello from upstream"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}

	// StoreTransaction is synchronous inside handleHTTP, so the DB row is
	// already committed by the time client.Get returns.
	reqs, _, err := eng.DB().ListTransactions(10, 0, "", "", 0)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(reqs) == 0 {
		t.Error("expected at least one stored transaction, got none")
	}
}

// TestHTTPProxy_PluginModifiesRequestHeader verifies that a plugin can inject a
// custom header that reaches the upstream server.
func TestHTTPProxy_PluginModifiesRequestHeader(t *testing.T) {
	t.Parallel()

	var receivedHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Plugin")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, rt := newTestStack(t)
	startAutoResume(t, bpMgr)

	if err := rt.Register(&testPlugin{
		name: "req-header-injector",
		onReq: func(req *plugins.ProxyRequest) *plugins.ProxyRequest {
			clone := req.Clone(req.Body)
			clone.Headers.Set("X-Plugin", "injected")
			return clone
		},
	}); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	if receivedHeader != "injected" {
		t.Errorf("X-Plugin header at upstream: got %q, want %q", receivedHeader, "injected")
	}
}

// TestHTTPProxy_PluginModifiesResponseHeader verifies that a plugin can inject
// a response header that is forwarded to the client.
func TestHTTPProxy_PluginModifiesResponseHeader(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, rt := newTestStack(t)
	startAutoResume(t, bpMgr)

	if err := rt.Register(&testPlugin{
		name: "resp-header-injector",
		onResp: func(resp *plugins.ProxyResponse) *plugins.ProxyResponse {
			clone := resp.Clone(resp.Body)
			clone.Headers.Set("X-Plugin-Response", "yes")
			return clone
		},
	}); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Plugin-Response"); got != "yes" {
		t.Errorf("X-Plugin-Response header: got %q, want %q", got, "yes")
	}
}

// TestHTTPProxy_PostBodyStoredAndReplayed is an end-to-end test that sends a
// POST with a JSON body through the proxy, verifies the body is persisted in
// storage, then replays the stored request and confirms the upstream receives
// the original body unchanged.
func TestHTTPProxy_PostBodyStoredAndReplayed(t *testing.T) {
	t.Parallel()

	const wantBody = `{"key":"value"}`
	var upstreamReceivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamReceivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, eng, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
		},
	}

	// Send POST with a JSON body through the proxy.
	resp, err := client.Post(upstream.URL+"/api", "application/json", strings.NewReader(wantBody))
	if err != nil {
		t.Fatalf("POST through proxy: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	// Verify the upstream received the body during the proxied request.
	if upstreamReceivedBody != wantBody {
		t.Errorf("upstream received body during proxy: got %q, want %q", upstreamReceivedBody, wantBody)
	}

	// Verify the body was persisted in storage.
	reqs, _, err := eng.DB().ListTransactions(10, 0, "", "POST", 0)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if len(reqs) == 0 {
		t.Fatal("no POST transaction found in storage")
	}
	if string(reqs[0].Body) != wantBody {
		t.Errorf("stored request body: got %q, want %q", string(reqs[0].Body), wantBody)
	}

	// Replay the stored request and verify the upstream receives the same body.
	upstreamReceivedBody = ""
	replayEng := replayengine.NewEngine(eng.DB(), nil)
	replayResp, err := replayEng.ReplayRequest(context.Background(), &replayengine.ReplayRequest{
		RequestID:       reqs[0].ID,
		FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	replayResp.Body.Close()

	if upstreamReceivedBody != wantBody {
		t.Errorf("upstream received body during replay: got %q, want %q", upstreamReceivedBody, wantBody)
	}
}

// breakpoint, waits for an explicit resume decision, and then delivers the
// correct upstream response to the client.
func TestHTTPProxy_BreakpointPausesAndForwards(t *testing.T) {
	t.Parallel()

	const wantBody = "breakpoint response"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, _ := newTestStack(t)
	// Intentionally no startAutoResume — we control the resume ourselves.

	// Register a breakpoint rule that matches any URL so the proxy will pause
	// the request below.
	if _, err := bpMgr.AddRule(&breakpoints.BreakpointRule{
		URLPattern: `.*`,
		Enabled:    true,
		Label:      "test-bp",
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	// Subscribe before the request goroutine is started to avoid a race.
	pausedCh := bpMgr.Subscribe()
	t.Cleanup(func() { bpMgr.Unsubscribe(pausedCh) })

	type result struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan result, 1)

	// This goroutine blocks inside the proxy at PauseRequest until we resume.
	go func() {
		c := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL)),
			},
		}
		resp, err := c.Get(upstream.URL)
		resultCh <- result{resp, err}
	}()

	// Wait for the proxy to pause the request.
	var entry *breakpoints.PausedEntry
	select {
	case entry = <-pausedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for request to be paused")
	}

	// Resume: forward to upstream unchanged.
	if err := bpMgr.Resume(entry.RequestID, &breakpoints.ResumeDecision{
		Action: breakpoints.ActionForward,
	}); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// Collect the final response.
	var res result
	select {
	case res = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for proxied response after resume")
	}

	if res.err != nil {
		t.Fatalf("GET through proxy: %v", res.err)
	}
	defer res.resp.Body.Close()

	if res.resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", res.resp.StatusCode)
	}
	body, _ := io.ReadAll(res.resp.Body)
	if string(body) != wantBody {
		t.Errorf("body: got %q, want %q", body, wantBody)
	}
}

// resumeWith is a helper that waits for a single paused request then resumes
// it with the given decision. It returns the paused entry's request ID.
func resumeWith(t *testing.T, bpMgr *breakpoints.Manager, decision *breakpoints.ResumeDecision) string {
	t.Helper()
	ch := bpMgr.Subscribe()
	t.Cleanup(func() { bpMgr.Unsubscribe(ch) })

	var entry *breakpoints.PausedEntry
	select {
	case entry = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for paused request")
	}
	if err := bpMgr.Resume(entry.RequestID, decision); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	return entry.RequestID
}

// addCatchAllBreakpoint adds an enabled breakpoint rule that matches all URLs.
func addCatchAllBreakpoint(t *testing.T, bpMgr *breakpoints.Manager) {
	t.Helper()
	if _, err := bpMgr.AddRule(&breakpoints.BreakpointRule{
		URLPattern: `.*`,
		Enabled:    true,
		Label:      "catch-all",
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
}

// TestHTTPProxy_BreakpointDrop verifies that ActionDrop returns 502 to the
// client and that the upstream never receives the request.
func TestHTTPProxy_BreakpointDrop(t *testing.T) {
	t.Parallel()

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, _ := newTestStack(t)
	addCatchAllBreakpoint(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	type result struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}
		resp, err := c.Get(upstream.URL)
		resultCh <- result{resp, err}
	}()

	// Resume with DROP action.
	resumeWith(t, bpMgr, &breakpoints.ResumeDecision{Action: breakpoints.ActionDrop})

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for client response")
	}
	if res.err != nil {
		t.Fatalf("GET: %v", res.err)
	}
	defer res.resp.Body.Close()
	if res.resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502 (dropped)", res.resp.StatusCode)
	}
	if upstreamCalled {
		t.Error("upstream should NOT have been called for a dropped request")
	}
}

// TestHTTPProxy_BreakpointRespond verifies that ActionRespond returns the
// synthetic response to the client and that the upstream is never called.
func TestHTTPProxy_BreakpointRespond(t *testing.T) {
	t.Parallel()

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, _ := newTestStack(t)
	addCatchAllBreakpoint(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	type result struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}
		resp, err := c.Get(upstream.URL)
		resultCh <- result{resp, err}
	}()

	syntheticBody := `{"synthetic":true}`
	resumeWith(t, bpMgr, &breakpoints.ResumeDecision{
		Action: breakpoints.ActionRespond,
		ModifiedResponse: &http.Response{
			StatusCode: http.StatusTeapot,
			Status:     "418 I'm a teapot",
			Header:     http.Header{"X-Source": []string{"breakpoint"}},
			Body:       io.NopCloser(bytes.NewBufferString(syntheticBody)),
		},
	})

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for client response")
	}
	if res.err != nil {
		t.Fatalf("GET: %v", res.err)
	}
	defer res.resp.Body.Close()

	if res.resp.StatusCode != http.StatusTeapot {
		t.Errorf("status: got %d, want 418", res.resp.StatusCode)
	}
	if got := res.resp.Header.Get("X-Source"); got != "breakpoint" {
		t.Errorf("X-Source: got %q want %q", got, "breakpoint")
	}
	body, _ := io.ReadAll(res.resp.Body)
	if string(body) != syntheticBody {
		t.Errorf("body: got %q, want %q", string(body), syntheticBody)
	}
	if upstreamCalled {
		t.Error("upstream should NOT have been called for a responded request")
	}
}

// TestHTTPProxy_BreakpointModifiesRequest verifies that ActionForward with a
// ModifiedRequest causes the upstream to receive the modified URL/headers.
func TestHTTPProxy_BreakpointModifiesRequest(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedHeader = r.Header.Get("X-Modified")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, _ := newTestStack(t)
	addCatchAllBreakpoint(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	type result struct {
		resp *http.Response
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}
		resp, err := c.Get(upstream.URL + "/original")
		resultCh <- result{resp, err}
	}()

	modifiedURL := mustParseURL(t, upstream.URL+"/modified")
	modifiedReq, _ := http.NewRequest(http.MethodGet, modifiedURL.String(), http.NoBody)
	modifiedReq.Header.Set("X-Modified", "yes")
	resumeWith(t, bpMgr, &breakpoints.ResumeDecision{
		Action:          breakpoints.ActionForward,
		ModifiedRequest: modifiedReq,
	})

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for client response")
	}
	if res.err != nil {
		t.Fatalf("GET: %v", res.err)
	}
	res.resp.Body.Close()

	if receivedPath != "/modified" {
		t.Errorf("upstream received path: got %q, want /modified", receivedPath)
	}
	if receivedHeader != "yes" {
		t.Errorf("upstream received X-Modified: got %q, want yes", receivedHeader)
	}
}

// TestHTTPProxy_MaxBodySizeEnforced verifies that a request body larger than
// MaxBodySizeMB is rejected with 413.
func TestHTTPProxy_MaxBodySizeEnforced(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

	dir := t.TempDir()
	ca, err := proxy.NewCertAuthority(
		filepath.Join(dir, "ca.crt"),
		filepath.Join(dir, "ca.key"),
	)
	if err != nil {
		t.Fatalf("NewCertAuthority: %v", err)
	}

	// Tiny 1-byte limit to make it easy to exceed.
	cfg := &config.Config{
		HTTPReadHeaderTimeout: 10,
		HTTPReadTimeout:       30,
		HTTPWriteTimeout:      120,
		HTTPIdleTimeout:       120,
		MaxBodySizeMB:         0, // 0 MB → effectively 0 bytes when set in bytes
	}
	// Override the byte limit to 1 byte via a small positive value
	// (MaxBodySizeMB: 0 means no limit; use 1 byte = ~0 MB rounded, so set 1)
	cfg.MaxBodySizeMB = 1 // 1 MB; we'll send more than 1 MB

	tlsP := proxy.NewTLSProxy(ca, eng, proxy.TransportOptions{}, cfg)
	tlsP.SetPlugins(rt)
	httpP := proxy.NewHTTPProxy("", tlsP, eng, proxy.TransportOptions{}, cfg)
	httpP.SetPlugins(rt)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	// Send a body larger than 1 MB.
	bigBody := bytes.Repeat([]byte("x"), 2*1024*1024)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}
	resp, err := client.Post(upstream.URL, "text/plain", bytes.NewReader(bigBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want 413", resp.StatusCode)
	}
}

// TestHTTPProxy_UpstreamUnreachable verifies that when the upstream is not
// reachable, the proxy returns 502 to the client.
func TestHTTPProxy_UpstreamUnreachable(t *testing.T) {
	t.Parallel()

	httpP, _, _, bpMgr, _ := newTestStack(t)
	startAutoResume(t, bpMgr)

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}

	// Port 1 is never bound in tests; dial will fail immediately.
	resp, err := client.Get("http://127.0.0.1:1/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status: got %d, want 502", resp.StatusCode)
	}
}

// TestHTTPProxy_PluginModifiesBody verifies that a plugin which replaces the
// request body causes the upstream to receive the new body.
func TestHTTPProxy_PluginModifiesBody(t *testing.T) {
	t.Parallel()

	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		upstreamBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	httpP, _, _, bpMgr, rt := newTestStack(t)
	startAutoResume(t, bpMgr)

	const original = "original body"
	const modified = "plugin modified body"

	if err := rt.Register(&testPlugin{
		name: "body-replacer",
		onReq: func(req *plugins.ProxyRequest) *plugins.ProxyRequest {
			clone := req.Clone(io.NopCloser(strings.NewReader(modified)))
			return clone
		},
	}); err != nil {
		t.Fatalf("register plugin: %v", err)
	}

	proxySrv := httptest.NewServer(httpP)
	t.Cleanup(proxySrv.Close)

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))}}
	resp, err := client.Post(upstream.URL, "text/plain", strings.NewReader(original))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	if upstreamBody != modified {
		t.Errorf("upstream body: got %q, want %q", upstreamBody, modified)
	}
}
