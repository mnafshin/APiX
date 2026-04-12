// Package resilience_test contains fault-injection and resilience tests that
// deliberately cause network failures, upstream errors, and slow conditions to
// verify APiX handles them gracefully. Tests exercise the proxy, replay engine,
// and engine pub/sub paths.
package resilience_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
)

// ---------------------------------------------------------------------------
// Shared stack helpers (mirrors tests/integration pattern)
// ---------------------------------------------------------------------------

type proxyStack struct {
	proxy    *proxy.HTTPProxy
	engine   *engine.Engine
	db       *storage.DB
	proxyURL string
	httpSrv  *http.Server
	once     sync.Once
}

func (s *proxyStack) stop() {
	s.once.Do(func() {
		s.httpSrv.Close()
		s.db.Close()
	})
}

func newStack(t *testing.T) *proxyStack {
	t.Helper()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

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
		HTTPReadHeaderTimeout: 5,
		HTTPReadTimeout:       10,
		HTTPWriteTimeout:      30,
		HTTPIdleTimeout:       60,
		MaxBodySizeMB:         4,
	}

	tlsP := proxy.NewTLSProxy(ca, eng, proxy.TransportOptions{
		DialTimeout:           2 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	}, cfg)
	tlsP.SetPlugins(rt)

	httpP := proxy.NewHTTPProxy("", tlsP, eng, proxy.TransportOptions{
		DialTimeout:           2 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	}, cfg)
	httpP.SetPlugins(rt)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		t.Fatalf("net.Listen: %v", err)
	}

	srv := &http.Server{Handler: httpP}
	go srv.Serve(ln) //nolint:errcheck

	// Small delay for the server goroutine to start.
	time.Sleep(20 * time.Millisecond)

	s := &proxyStack{
		proxy:    httpP,
		engine:   eng,
		db:       db,
		proxyURL: fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port),
		httpSrv:  srv,
	}
	t.Cleanup(s.stop)
	return s
}

// httpClientThrough builds an http.Client that routes all requests through the
// given proxy URL with the specified total timeout.
func httpClientThrough(proxyURL string, timeout time.Duration) *http.Client {
	u, _ := url.Parse(proxyURL)
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   timeout,
	}
}

// makeTx creates a minimal proxy.Transaction for direct engine calls.
func makeTx(id, method, rawURL string) *proxy.Transaction {
	req, _ := http.NewRequest(method, rawURL, nil)
	return &proxy.Transaction{
		ID: id,
		Request: &plugins.ProxyRequest{
			ID:      id,
			Method:  req.Method,
			URL:     req.URL,
			Headers: req.Header,
			Raw:     req,
		},
	}
}

// ---------------------------------------------------------------------------
// 1. Upstream abruptly resets the connection (hijack + close)
// ---------------------------------------------------------------------------

func TestResilience_UpstreamConnectionReset(t *testing.T) {
	stack := newStack(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", 500)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close() // write nothing and close — simulates a TCP RST
	}))
	defer upstream.Close()

	client := httpClientThrough(stack.proxyURL, 5*time.Second)
	resp, err := client.Get(upstream.URL + "/reset")

	// Proxy must not panic. Either an error or a 5xx gateway response is acceptable.
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("expected error or ≥400 for reset upstream, got %d", resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Upstream accepts the connection but never responds (read timeout)
// ---------------------------------------------------------------------------

func TestResilience_UpstreamTimeout(t *testing.T) {
	stack := newStack(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until client or server gives up
	}))
	defer upstream.Close()

	// Total client timeout shorter than any test timeout.
	client := httpClientThrough(stack.proxyURL, 5*time.Second)

	start := time.Now()
	resp, err := client.Get(upstream.URL + "/never")
	elapsed := time.Since(start)

	if err == nil {
		defer resp.Body.Close()
	}
	// The request must have resolved (error or response) well before the
	// proxy's ResponseHeaderTimeout (3 s) plus some margin.
	if elapsed > 8*time.Second {
		t.Fatalf("request should have timed out before 8 s, elapsed %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 3. Upstream sends a malformed HTTP response over a raw TCP connection
// ---------------------------------------------------------------------------

func TestResilience_MalformedUpstreamResponse(t *testing.T) {
	// Raw listener so we can write arbitrary bytes.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				c.Read(buf) //nolint:errcheck
				// Deliberately invalid HTTP/1.1 status line.
				c.Write([]byte("THIS IS NOT HTTP\r\n\r\n")) //nolint:errcheck
			}(conn)
		}
	}()

	stack := newStack(t)
	client := httpClientThrough(stack.proxyURL, 5*time.Second)
	resp, err := client.Get("http://" + ln.Addr().String() + "/malformed")

	// Either an error or a 4xx/5xx gateway response — must not panic.
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("expected error or ≥400 for malformed response, got %d", resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. Upstream sends the body one byte at a time (slow body streaming)
// ---------------------------------------------------------------------------

func TestResilience_SlowBodyUpstream(t *testing.T) {
	stack := newStack(t)

	payload := []byte("slow-streaming-body")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		flusher, hasFlusher := w.(http.Flusher)
		for _, b := range payload {
			w.Write([]byte{b}) //nolint:errcheck
			if hasFlusher {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	client := httpClientThrough(stack.proxyURL, 10*time.Second)
	resp, err := client.Get(upstream.URL + "/slow-body")
	if err != nil {
		t.Fatalf("unexpected error for slow body: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(payload) {
		t.Fatalf("expected payload %q, got %q", payload, body)
	}
}

// ---------------------------------------------------------------------------
// 5. Concurrent requests — one in three returns 500, rest succeed
// ---------------------------------------------------------------------------

func TestResilience_ConcurrentMixedResults(t *testing.T) {
	stack := newStack(t)

	var callCount int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n%3 == 0 {
			http.Error(w, "injected failure", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	}))
	defer upstream.Close()

	const total = 9
	statuses := make([]int, total)
	errs := make([]error, total)
	var wg sync.WaitGroup
	wg.Add(total)

	for i := 0; i < total; i++ {
		i := i
		go func() {
			defer wg.Done()
			c := httpClientThrough(stack.proxyURL, 5*time.Second)
			resp, err := c.Get(upstream.URL + "/concurrent")
			if err != nil {
				errs[i] = err
				return
			}
			statuses[i] = resp.StatusCode
			resp.Body.Close()
		}()
	}
	wg.Wait()

	ok, fail := 0, 0
	for i := 0; i < total; i++ {
		if errs[i] != nil {
			continue
		}
		if statuses[i] == http.StatusOK {
			ok++
		} else if statuses[i] == http.StatusInternalServerError {
			fail++
		}
	}

	if ok == 0 {
		t.Fatal("expected at least some successful (200) requests")
	}
	if fail == 0 {
		t.Fatal("expected at least some failed (500) requests")
	}
}

// ---------------------------------------------------------------------------
// 6. Replay engine surfaces timeout from a never-responding upstream
// ---------------------------------------------------------------------------

func TestResilience_ReplayUpstreamTimeout(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstream.Close()

	re := replay.NewEngine(db, &replay.ClientConfig{
		DialTimeout:           200 * time.Millisecond,
		ResponseHeaderTimeout: 300 * time.Millisecond,
	})

	rawReq, _ := http.NewRequest("GET", upstream.URL+"/timeout", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = re.ReplayRequest(ctx, &replay.ReplayRequest{RawRequest: rawReq})
	if err == nil {
		t.Fatal("expected timeout error from replay engine")
	}
}

// ---------------------------------------------------------------------------
// 7. Replay engine handles connection-refused gracefully (no panic)
// ---------------------------------------------------------------------------

func TestResilience_ReplayConnectionRefused(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	// Grab a free port then release it so it is definitely not listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	re := replay.NewEngine(db, &replay.ClientConfig{
		DialTimeout: 500 * time.Millisecond,
	})

	rawReq, _ := http.NewRequest("GET", "http://"+addr+"/refused", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = re.ReplayRequest(ctx, &replay.ReplayRequest{RawRequest: rawReq})
	if err == nil {
		t.Fatal("expected connection-refused error from replay engine")
	}
}

// ---------------------------------------------------------------------------
// 8. Engine: subscriber that disappears before traffic — no panic, no block
// ---------------------------------------------------------------------------

func TestResilience_SubscriberGoneBeforeTraffic(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

	// Subscribe then immediately unsubscribe before any traffic arrives.
	ch := eng.Subscribe()
	eng.Unsubscribe(ch)

	// Store a transaction — must not panic or block on the removed subscriber.
	tx := makeTx("gone-sub", "GET", "http://example.com/gone")
	if err := eng.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction after subscriber removed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 9. Engine: storing a transaction with no subscribers — no deadlock
// ---------------------------------------------------------------------------

func TestResilience_StoreTransactionNoSubscribers(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	bpMgr := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpMgr, rt)

	done := make(chan error, 1)
	go func() {
		done <- eng.StoreTransaction(makeTx("no-sub", "POST", "http://example.com/nosub"))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("StoreTransaction with no subscribers: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StoreTransaction blocked with no subscribers — possible deadlock")
	}
}
