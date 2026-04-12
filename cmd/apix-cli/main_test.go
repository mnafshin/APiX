package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/proxy"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/server"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/plugins"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"google.golang.org/grpc"
)

type cliTestStack struct {
	db       *storage.DB
	eng      *engine.Engine
	bpm      *breakpoints.Manager
	grpcSrv  *grpc.Server
	listener net.Listener
	host     string
	port     int
	token    string
}

func newCLITestStack(t *testing.T, authToken string) *cliTestStack {
	t.Helper()

	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	bpm := breakpoints.NewManager()
	rt := pluginrt.NewRuntime()
	eng := engine.New(db, bpm, rt)
	re := replay.NewEngine(db, nil)
	cfg := &config.Config{
		HTTPPort: "8080",
		GRPCPort: "9090",
		AuthToken: authToken,
	}

	grpcSrv := server.NewGRPCServer(cfg)
	apix.RegisterEngineServer(grpcSrv, server.NewEngineServer(eng, re, cfg))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go grpcSrv.Serve(lis) //nolint:errcheck
	t.Cleanup(func() {
		grpcSrv.Stop()
		_ = lis.Close()
		_ = db.Close()
	})

	addr := lis.Addr().(*net.TCPAddr)
	return &cliTestStack{
		db:       db,
		eng:      eng,
		bpm:      bpm,
		grpcSrv:  grpcSrv,
		listener: lis,
		host:     "127.0.0.1",
		port:     addr.Port,
		token:    authToken,
	}
}

func (s *cliTestStack) args(extra ...string) []string {
	args := []string{"--host", s.host, "--port", fmt.Sprintf("%d", s.port)}
	if s.token != "" {
		args = append(args, "--token", s.token)
	}
	return append(args, extra...)
}

func (s *cliTestStack) storeTransaction(t *testing.T, id, method, rawURL, reqBody, respBody string, statusCode int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rawReq, err := http.NewRequest("GET", rawURL, io.NopCloser(strings.NewReader(reqBody)))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	rawReq.Method = method
	tx := &proxy.Transaction{
		ID: id,
		Request: &plugins.ProxyRequest{
			ID:      id,
			Method:  method,
			URL:     parsed,
			Headers: http.Header{"X-Test": []string{"1"}},
			Body:    io.NopCloser(strings.NewReader(reqBody)),
			Raw:     rawReq,
		},
		RequestBody: reqBodyBytes(reqBody),
		DurationMs:  12,
	}
	if statusCode > 0 {
		tx.Response = &plugins.ProxyResponse{
			StatusCode: statusCode,
			Status:     fmt.Sprintf("%d status", statusCode),
			Headers:    http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}
		tx.ResponseBody = []byte(respBody)
	}
	if err := s.eng.StoreTransaction(tx); err != nil {
		t.Fatalf("StoreTransaction: %v", err)
	}
}

func reqBodyBytes(body string) []byte {
	if body == "" {
		return nil
	}
	return []byte(body)
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	exit := Run(args, &out, &errb)
	return exit, out.String(), errb.String()
}

func TestCLIStatusJSON_RealServer(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, out, errOut := runCLI(t, stack.args("--output", "json", "status")...)
	if exit != 0 {
		t.Fatalf("exit=%d err=%s", exit, errOut)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload["status"] != "OK" {
		t.Fatalf("status=%v", payload["status"])
	}
}

func TestCLIReadWorkflows_EngineBacked(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")
	stack.storeTransaction(t, "req-1", "POST", "https://example.com/users", `{"name":"A"}`, `{"ok":true}`, 201)

	exit, out, errOut := runCLI(t, stack.args("--output", "json", "plugins", "list")...)
	if exit != 0 {
		t.Fatalf("plugins exit=%d err=%s", exit, errOut)
	}
	var pluginsPayload []any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &pluginsPayload); err != nil {
		t.Fatalf("plugins json: %v", err)
	}

	exit, out, errOut = runCLI(t, stack.args("--output", "json", "history", "list", "--limit", "10")...)
	if exit != 0 {
		t.Fatalf("history list exit=%d err=%s", exit, errOut)
	}
	var historyList []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &historyList); err != nil {
		t.Fatalf("history list json: %v", err)
	}
	if len(historyList) != 1 || historyList[0]["id"] != "req-1" {
		t.Fatalf("unexpected history list: %#v", historyList)
	}

	exit, out, errOut = runCLI(t, stack.args("--output", "json", "history", "get", "req-1")...)
	if exit != 0 {
		t.Fatalf("history get exit=%d err=%s", exit, errOut)
	}
	var historyItem map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &historyItem); err != nil {
		t.Fatalf("history get json: %v", err)
	}
	if historyItem["id"] != "req-1" {
		t.Fatalf("history id=%v", historyItem["id"])
	}
}

func TestCLIWatchNDJSON_RealTraffic(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	done := make(chan struct{})
	var exit int
	var out, errOut string
	go func() {
		exit, out, errOut = runCLI(t, stack.args("--output", "ndjson", "watch", "traffic", "--count", "1")...)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	stack.storeTransaction(t, "watch-1", "GET", "https://watch.example.com/ping", "", "pong", 200)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watch command did not finish")
	}
	if exit != 0 {
		t.Fatalf("watch exit=%d err=%s", exit, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 ndjson line, got %d", len(lines))
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("watch json: %v", err)
	}
	if event["id"] != "watch-1" {
		t.Fatalf("event id=%v", event["id"])
	}
}
