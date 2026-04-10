// Package e2e_test contains full-stack integration tests that wire the real
// proxy, engine, storage, and gRPC server together with no mocks.
package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	iproxy "github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/server"
	"github.com/mnafshin/apix/internal/storage"
	apix "github.com/mnafshin/apix/pkg/api/generated"
)

// testStack bundles all running components for a single e2e test scenario.
type testStack struct {
	proxyURL string
	client   apix.EngineClient
	db       *storage.DB
	bpm      *breakpoints.Manager
	eng      *engine.Engine
	stop     func()
}

// startStack spins up the full stack: in-memory DB → breakpoints → pluginrt →
// engine → replay → gRPC server → HTTP proxy, all on random OS-assigned ports.
func startStack(t *testing.T) *testStack {
	t.Helper()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	bpm := breakpoints.NewManager()
	prt := pluginrt.NewRuntime()
	eng := engine.New(db, bpm, prt)
	re := replay.NewEngine(db, nil)

	// gRPC server on a random port.
	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		t.Fatalf("grpc listen: %v", err)
	}
	grpcPort := grpcLis.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{
		HTTPPort:              "0",
		GRPCPort:              fmt.Sprintf("%d", grpcPort),
		HTTPReadHeaderTimeout: 10,
		HTTPReadTimeout:       30,
		HTTPWriteTimeout:      120,
		MaxBodySizeMB:         32,
	}
	grpcSrv := grpc.NewServer()
	apix.RegisterEngineServer(grpcSrv, server.NewEngineServer(eng, re, cfg))
	go grpcSrv.Serve(grpcLis) //nolint:errcheck

	// HTTP proxy on a random port — use a pre-allocated listener so we know
	// the port before Serve blocks.
	proxyLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		grpcSrv.Stop()
		db.Close()
		t.Fatalf("proxy listen: %v", err)
	}
	proxyPort := proxyLis.Addr().(*net.TCPAddr).Port
	httpProxy := iproxy.NewHTTPProxy("", nil, eng, iproxy.TransportOptions{}, cfg)
	httpSrv := &http.Server{Handler: httpProxy}
	go httpSrv.Serve(proxyLis) //nolint:errcheck

	// gRPC client.
	conn, err := grpc.NewClient(
		fmt.Sprintf("127.0.0.1:%d", grpcPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		_ = httpSrv.Close()
		grpcSrv.Stop()
		db.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	return &testStack{
		proxyURL: fmt.Sprintf("http://127.0.0.1:%d", proxyPort),
		client:   apix.NewEngineClient(conn),
		db:       db,
		bpm:      bpm,
		eng:      eng,
		stop: func() {
			conn.Close()
			grpcSrv.Stop()
			_ = httpSrv.Close()
			db.Close()
		},
	}
}

// proxyHTTPClient returns an *http.Client that routes all requests through the
// given forward-proxy URL.
func proxyHTTPClient(proxyURL string) *http.Client {
	u, _ := url.Parse(proxyURL)
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   15 * time.Second,
	}
}

// drainHistory reads all HttpTransaction messages until EOF and returns them.
func drainHistory(t *testing.T, stream grpc.ServerStreamingClient[apix.HttpTransaction]) []*apix.HttpTransaction {
	t.Helper()
	var txs []*apix.HttpTransaction
	for {
		tx, err := stream.Recv()
		if err != nil {
			break
		}
		txs = append(txs, tx)
	}
	return txs
}

// ── Test 1: HTTP proxy captures traffic and exposes it via GetHistory ──────

func TestE2E_HTTPProxyAndHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	// Mock upstream returns a fixed body.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello from upstream")
	}))
	defer upstream.Close()

	// Send a GET request through the proxy.
	client := proxyHTTPClient(stack.proxyURL)
	resp, err := client.Get(upstream.URL + "/test")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got := string(body); got != "hello from upstream" {
		t.Errorf("response body: got %q want %q", got, "hello from upstream")
	}

	// The proxy stores the transaction before writing the response, so by the
	// time client.Get returns the record is in the DB.  A small sleep guards
	// against any residual OS scheduling delay.
	time.Sleep(50 * time.Millisecond)

	// Verify the transaction appears in GetHistory via gRPC.
	stream, err := stack.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	txs := drainHistory(t, stream)
	if len(txs) == 0 {
		t.Fatal("expected at least 1 transaction in history, got 0")
	}
	var found bool
	for _, tx := range txs {
		if tx.Request != nil && strings.Contains(tx.Request.Url, "/test") {
			found = true
		}
	}
	if !found {
		t.Errorf("did not find /test in history; %d transactions returned", len(txs))
	}
}

// ── Test 2: Breakpoint pauses a proxied request; ResumeRequest unblocks it ─

func TestE2E_BreakpointPauseAndResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "breakpoint response")
	}))
	defer upstream.Close()

	// Set a breakpoint that matches the /pause path.
	_, err := stack.client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern: `.*pause.*`,
		Methods:    []string{"GET"},
		Enabled:    true,
		Label:      "e2e-bp",
	})
	if err != nil {
		t.Fatalf("SetBreakpoint: %v", err)
	}

	// Subscribe to paused-request notifications before the request arrives.
	watchStream, err := stack.client.WatchPausedRequests(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("WatchPausedRequests: %v", err)
	}
	// Give the gRPC server goroutine time to register its subscription.
	time.Sleep(50 * time.Millisecond)

	// Send the request through the proxy in a goroutine; it will block at the
	// breakpoint until someone calls ResumeRequest.
	type result struct {
		body string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := proxyHTTPClient(stack.proxyURL)
		r, err := c.Get(upstream.URL + "/pause")
		if err != nil {
			resultCh <- result{err: err}
			return
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		resultCh <- result{body: string(b), err: err}
	}()

	// Receive the paused-request notification via gRPC.
	paused, err := watchStream.Recv()
	if err != nil {
		t.Fatalf("WatchPausedRequests.Recv: %v", err)
	}
	if paused.RequestId == "" {
		t.Error("expected non-empty RequestId in paused notification")
	}
	if paused.Request == nil || !strings.Contains(paused.Request.Url, "/pause") {
		t.Errorf("unexpected paused URL: %q", paused.GetRequest().GetUrl())
	}

	// Resume the request with FORWARD so the proxy forwards it to the upstream.
	if _, err := stack.client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: paused.RequestId,
		Action:    apix.ResumeAction_FORWARD,
	}); err != nil {
		t.Fatalf("ResumeRequest: %v", err)
	}

	// Wait for the proxied request goroutine to complete.
	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("proxy GET after resume: %v", res.err)
		}
		if res.body != "breakpoint response" {
			t.Errorf("body: got %q want %q", res.body, "breakpoint response")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for proxy request to complete after resume")
	}
}

// ── Test 3: Captured request can be replayed with header overrides ─────────

func TestE2E_ReplayRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	// Track whether the upstream received the X-Replayed header.
	var (
		mu             sync.Mutex
		replayReceived bool
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Replayed") == "true" {
			mu.Lock()
			replayReceived = true
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "replay response")
	}))
	defer upstream.Close()

	// Capture a request through the proxy.
	client := proxyHTTPClient(stack.proxyURL)
	resp, err := client.Get(upstream.URL + "/replay-me")
	if err != nil {
		t.Fatalf("proxy GET: %v", err)
	}
	resp.Body.Close()

	time.Sleep(50 * time.Millisecond)

	// Retrieve the stored transaction ID.
	histStream, err := stack.client.GetHistory(ctx, &apix.HistoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	txs := drainHistory(t, histStream)
	if len(txs) == 0 {
		t.Fatal("no transactions in history after proxy request")
	}
	txID := txs[0].Id

	// Replay with an override header; the replay engine contacts the upstream
	// directly (bypassing the proxy) so it uses the stored URL unchanged.
	replayResp, err := stack.client.ReplayRequest(ctx, &apix.ReplaySpec{
		Source:          &apix.ReplaySpec_RequestId{RequestId: txID},
		OverrideHeaders: map[string]string{"X-Replayed": "true"},
	})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	if replayResp.StatusCode != http.StatusOK {
		t.Errorf("replay status: got %d want %d", replayResp.StatusCode, http.StatusOK)
	}

	mu.Lock()
	got := replayReceived
	mu.Unlock()
	if !got {
		t.Error("upstream did not receive X-Replayed: true on the replayed request")
	}
}

// ── Test 4: Breakpoint DROP returns 502 to client, upstream not called ──────

func TestE2E_BreakpointDrop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	_, err := stack.client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern: `.*drop.*`,
		Enabled:    true,
		Label:      "e2e-drop",
	})
	if err != nil {
		t.Fatalf("SetBreakpoint: %v", err)
	}

	watchStream, err := stack.client.WatchPausedRequests(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("WatchPausedRequests: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	type result struct {
		status int
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := proxyHTTPClient(stack.proxyURL)
		r, err := c.Get(upstream.URL + "/drop")
		if err != nil {
			resultCh <- result{err: err}
			return
		}
		r.Body.Close()
		resultCh <- result{status: r.StatusCode}
	}()

	paused, err := watchStream.Recv()
	if err != nil {
		t.Fatalf("WatchPausedRequests.Recv: %v", err)
	}

	_, err = stack.client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: paused.RequestId,
		Action:    apix.ResumeAction_DROP,
	})
	if err != nil {
		t.Fatalf("ResumeRequest DROP: %v", err)
	}

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for proxy response")
	}
	if res.err != nil {
		t.Fatalf("proxy GET: %v", res.err)
	}
	if res.status != http.StatusBadGateway {
		t.Errorf("status: got %d want 502 (dropped)", res.status)
	}
	if upstreamCalled {
		t.Error("upstream should NOT have been called for a dropped request")
	}
}

// ── Test 5: Breakpoint RESPOND delivers synthetic response; upstream not called

func TestE2E_BreakpointRespond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	_, err := stack.client.SetBreakpoint(ctx, &apix.BreakpointRule{
		UrlPattern: `.*respond.*`,
		Enabled:    true,
		Label:      "e2e-respond",
	})
	if err != nil {
		t.Fatalf("SetBreakpoint: %v", err)
	}

	watchStream, err := stack.client.WatchPausedRequests(ctx, &apix.Empty{})
	if err != nil {
		t.Fatalf("WatchPausedRequests: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	type result struct {
		status int
		body   string
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		c := proxyHTTPClient(stack.proxyURL)
		r, err := c.Get(upstream.URL + "/respond")
		if err != nil {
			resultCh <- result{err: err}
			return
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		resultCh <- result{status: r.StatusCode, body: string(b)}
	}()

	paused, err := watchStream.Recv()
	if err != nil {
		t.Fatalf("WatchPausedRequests.Recv: %v", err)
	}

	_, err = stack.client.ResumeRequest(ctx, &apix.ResumeAction{
		RequestId: paused.RequestId,
		Action:    apix.ResumeAction_RESPOND,
		ModifiedResponse: &apix.HttpResponse{
			StatusCode: http.StatusTeapot,
			StatusText: "418 I'm a teapot",
			Body:       []byte(`{"synthetic":true}`),
		},
	})
	if err != nil {
		t.Fatalf("ResumeRequest RESPOND: %v", err)
	}

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for proxy response")
	}
	if res.err != nil {
		t.Fatalf("proxy GET: %v", res.err)
	}
	if res.status != http.StatusTeapot {
		t.Errorf("status: got %d want 418", res.status)
	}
	if !strings.Contains(res.body, "synthetic") {
		t.Errorf("body: got %q want synthetic content", res.body)
	}
	if upstreamCalled {
		t.Error("upstream should NOT have been called for a responded request")
	}
}

// ── Test 6: Concurrent requests all captured in history ────────────────────

func TestE2E_ConcurrentRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stack := startStack(t)
	t.Cleanup(stack.stop)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c := proxyHTTPClient(stack.proxyURL)
			r, err := c.Get(upstream.URL + "/concurrent")
			if err == nil {
				r.Body.Close()
			}
		}()
	}
	wg.Wait()

	time.Sleep(100 * time.Millisecond)

	stream, err := stack.client.GetHistory(ctx, &apix.HistoryQuery{Limit: int32(n * 2)})
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	txs := drainHistory(t, stream)
	if len(txs) < n {
		t.Errorf("expected at least %d transactions after %d concurrent requests, got %d", n, n, len(txs))
	}
}
