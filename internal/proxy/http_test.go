package proxy_test

import (
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
