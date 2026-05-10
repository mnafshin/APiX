//go:build integration

// Package integration_test contains Testcontainers-backed integration tests.
//
// These tests spin up real Docker containers (e.g., kennethreitz/httpbin) to
// act as upstream services, then route traffic through the APiX proxy+engine
// stack and verify end-to-end capture, persistence, and replay.
//
// Run these tests with:
//
//	go test ./tests/integration -v -tags=integration -run TestContainers
//
// Docker must be available. Tests are skipped automatically when Docker is not
// reachable, so they are safe to include in CI without hard-failing on
// environments that lack Docker.
package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
)

// ---------------------------------------------------------------------------
// Container + proxy helpers
// ---------------------------------------------------------------------------

// httpbinContainer holds a running httpbin container and its HTTP URL.
type httpbinContainer struct {
	container testcontainers.Container
	URL       string
}

// startHTTPBin starts a kennethreitz/httpbin container and returns its base
// URL. The container is terminated when the test ends.
func startHTTPBin(t *testing.T) *httpbinContainer {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "kennethreitz/httpbin:latest",
		ExposedPorts: []string{"80/tcp"},
		WaitingFor:   wait.ForHTTP("/get").WithPort("80/tcp").WithStartupTimeout(60 * time.Second),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skip("Docker not available:", err)
		}
		t.Fatalf("start httpbin container: %v", err)
	}
	t.Cleanup(func() { c.Terminate(ctx) }) //nolint:errcheck

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "80/tcp")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	return &httpbinContainer{
		container: c,
		URL:       fmt.Sprintf("http://%s:%s", host, port.Port()),
	}
}

// isDockerUnavailable returns true for errors that indicate Docker is not
// installed or running. Tests are skipped rather than failed in those cases.
func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot connect to the Docker daemon") ||
		strings.Contains(msg, "docker: command not found") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial unix") ||
		strings.Contains(msg, "connection refused")
}

// containerProxyStack mirrors proxyStack (tests/integration/proxy_storage_test.go)
// but is defined here to keep the build tag isolated.
type containerProxyStack struct {
	engine   *engine.Engine
	db       *storage.DB
	httpSrv  *http.Server
	proxyURL string
	once     sync.Once
}

func (s *containerProxyStack) stop() {
	s.once.Do(func() {
		s.httpSrv.Close()
		s.db.Close()
	})
}

func newContainerProxyStack(t *testing.T) *containerProxyStack {
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
		HTTPReadHeaderTimeout: 10,
		HTTPReadTimeout:       30,
		HTTPWriteTimeout:      60,
		HTTPIdleTimeout:       120,
		MaxBodySizeMB:         4,
	}

	tlsP := proxy.NewTLSProxy(ca, eng, proxy.TransportOptions{}, cfg)
	tlsP.SetPlugins(rt)

	httpP := proxy.NewHTTPProxy("", tlsP, eng, proxy.TransportOptions{}, cfg)
	httpP.SetPlugins(rt)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		db.Close()
		t.Fatalf("net.Listen: %v", err)
	}

	srv := &http.Server{Handler: httpP}
	go srv.Serve(ln) //nolint:errcheck
	time.Sleep(30 * time.Millisecond)

	s := &containerProxyStack{
		engine:   eng,
		db:       db,
		httpSrv:  srv,
		proxyURL: fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port),
	}
	t.Cleanup(s.stop)
	return s
}

func proxyClient(proxyURL string) *http.Client {
	u, _ := url.Parse(proxyURL)
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   15 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestContainers_ProxyCapturesHTTPBinRequest sends a GET /get request through
// the APiX proxy to a real httpbin container and verifies the transaction is
// stored in SQLite.
func TestContainers_ProxyCapturesHTTPBinRequest(t *testing.T) {
	hb := startHTTPBin(t)
	stack := newContainerProxyStack(t)

	client := proxyClient(stack.proxyURL)
	resp, err := client.Get(hb.URL + "/get")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from httpbin, got %d: %s", resp.StatusCode, body)
	}

	// Give the engine a moment to persist.
	time.Sleep(300 * time.Millisecond)

	txs, _, err := stack.db.ListTransactions(10, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(txs) == 0 {
		t.Fatal("expected at least one stored transaction")
	}

	found := false
	for _, tx := range txs {
		if strings.Contains(tx.URL, "/get") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("GET /get not found in stored transactions: %+v", txs)
	}
}

// TestContainers_ProxyCapturesPostBody sends a POST /post with a JSON body
// and verifies the request body is stored along with the transaction.
func TestContainers_ProxyCapturesPostBody(t *testing.T) {
	hb := startHTTPBin(t)
	stack := newContainerProxyStack(t)

	client := proxyClient(stack.proxyURL)
	resp, err := client.Post(
		hb.URL+"/post",
		"application/json",
		strings.NewReader(`{"hello":"testcontainers"}`),
	)
	if err != nil {
		t.Fatalf("POST through proxy: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	time.Sleep(300 * time.Millisecond)

	txs, _, err := stack.db.ListTransactions(10, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}

	found := false
	for _, tx := range txs {
		if strings.Contains(tx.URL, "/post") && len(tx.Body) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("POST /post with body not found in stored transactions: %+v", txs)
	}
}

// TestContainers_ReplayFromStoredTransaction sends a request through the
// proxy, loads it from storage by ID, and replays it against the container.
func TestContainers_ReplayFromStoredTransaction(t *testing.T) {
	hb := startHTTPBin(t)
	stack := newContainerProxyStack(t)

	client := proxyClient(stack.proxyURL)
	resp, err := client.Get(hb.URL + "/uuid")
	if err != nil {
		t.Fatalf("GET /uuid through proxy: %v", err)
	}
	resp.Body.Close()

	time.Sleep(300 * time.Millisecond)

	txs, _, err := stack.db.ListTransactions(10, 0, "", "", 0, "")
	if err != nil || len(txs) == 0 {
		t.Fatalf("no stored transactions: %v", err)
	}

	var txID string
	for _, tx := range txs {
		if strings.Contains(tx.URL, "/uuid") {
			txID = tx.ID
			break
		}
	}
	if txID == "" {
		t.Fatal("/uuid transaction not found in storage")
	}

	// Replay the stored transaction.
	re := replay.NewEngine(stack.db, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	replayResp, err := re.ReplayRequest(ctx, &replay.ReplayRequest{RequestID: txID})
	if err != nil {
		t.Fatalf("ReplayRequest: %v", err)
	}
	defer replayResp.Body.Close()

	if replayResp.StatusCode != 200 {
		t.Fatalf("replay returned %d, expected 200", replayResp.StatusCode)
	}
}

// TestContainers_MultipleRequestsDifferentEndpoints sends requests to
// multiple httpbin endpoints and verifies all are stored.
func TestContainers_MultipleRequestsDifferentEndpoints(t *testing.T) {
	hb := startHTTPBin(t)
	stack := newContainerProxyStack(t)

	endpoints := []string{"/get", "/headers", "/ip", "/user-agent"}
	client := proxyClient(stack.proxyURL)

	for _, ep := range endpoints {
		resp, err := client.Get(hb.URL + ep)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		resp.Body.Close()
	}

	time.Sleep(500 * time.Millisecond)

	txs, _, err := stack.db.ListTransactions(20, 0, "", "", 0, "")
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}

	stored := make(map[string]bool)
	for _, tx := range txs {
		for _, ep := range endpoints {
			if strings.HasSuffix(tx.URL, ep) {
				stored[ep] = true
			}
		}
	}

	for _, ep := range endpoints {
		if !stored[ep] {
			t.Errorf("expected %s to be stored, but it was not", ep)
		}
	}
}
