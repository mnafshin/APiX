package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/plugins"
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
		HTTPPort:  "8080",
		GRPCPort:  "9090",
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
			ID:     id,
			Method: method,
			URL:    parsed,
			Headers: http.Header{
				"X-Test":       []string{"1"},
				"X-Request-ID": []string{"rid-" + id},
			},
			Body: io.NopCloser(strings.NewReader(reqBody)),
			Raw:  rawReq,
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

func parseErrorEnvelope(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		t.Fatalf("parse error envelope: %v; raw=%q", err, raw)
	}
	return payload
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

func TestCLIVerbose_EmitsDiagnostics(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, out, errOut := runCLI(t, stack.args("--verbose", "--output", "json", "status")...)
	if exit != 0 {
		t.Fatalf("exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(errOut, "[debug] running command: status") {
		t.Fatalf("missing command start diagnostic: %s", errOut)
	}
	if !strings.Contains(errOut, "[debug] command status finished in") {
		t.Fatalf("missing command end diagnostic: %s", errOut)
	}
	if !strings.Contains(errOut, "[debug] dialing engine:") {
		t.Fatalf("missing dial diagnostic: %s", errOut)
	}
	if !strings.Contains(errOut, "[debug] unary timeout=") {
		t.Fatalf("missing timeout diagnostic: %s", errOut)
	}
	if !strings.Contains(out, `"status":"OK"`) {
		t.Fatalf("unexpected status output: %s", out)
	}
}

func TestCLIDebugShortFlag_EmitsDiagnostics(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, _, errOut := runCLI(t, stack.args("--debug", "status")...)
	if exit != 0 {
		t.Fatalf("exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(errOut, "[debug] running command: status") {
		t.Fatalf("missing debug diagnostics: %s", errOut)
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
	if historyList[0]["request_id"] != "rid-req-1" {
		t.Fatalf("history list request_id=%v", historyList[0]["request_id"])
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
	if historyItem["request_id"] != "rid-req-1" {
		t.Fatalf("history request_id=%v", historyItem["request_id"])
	}

	exit, out, errOut = runCLI(t, stack.args("--output", "json", "history", "get", "--request-id", "rid-req-1")...)
	if exit != 0 {
		t.Fatalf("history get --request-id exit=%d err=%s", exit, errOut)
	}
	var historyByRequestID map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &historyByRequestID); err != nil {
		t.Fatalf("history get --request-id json: %v", err)
	}
	if historyByRequestID["id"] != "req-1" {
		t.Fatalf("history get --request-id id=%v", historyByRequestID["id"])
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

func TestCLIWriteWorkflows_EngineBacked(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, out, errOut := runCLI(t, stack.args("--output", "json", "breakpoints", "add", "--url-pattern", "example.com", "--method", "GET", "--label", "bp1")...)
	if exit != 0 {
		t.Fatalf("breakpoints add exit=%d err=%s", exit, errOut)
	}
	var bp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &bp); err != nil {
		t.Fatalf("breakpoint json: %v", err)
	}
	id := bp["id"].(string)

	exit, _, errOut = runCLI(t, stack.args("breakpoints", "disable", id)...)
	if exit != 0 {
		t.Fatalf("breakpoints disable exit=%d err=%s", exit, errOut)
	}
	exit, _, errOut = runCLI(t, stack.args("breakpoints", "enable", id)...)
	if exit != 0 {
		t.Fatalf("breakpoints enable exit=%d err=%s", exit, errOut)
	}
	exit, _, errOut = runCLI(t, stack.args("breakpoints", "delete", id)...)
	if exit != 0 {
		t.Fatalf("breakpoints delete exit=%d err=%s", exit, errOut)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("pong"))
	}))
	defer upstream.Close()

	exit, out, errOut = runCLI(t, stack.args("--output", "json", "send", "--method", "GET", "--url", upstream.URL+"/send")...)
	if exit != 0 {
		t.Fatalf("send exit=%d err=%s", exit, errOut)
	}
	var sendResp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &sendResp); err != nil {
		t.Fatalf("send json: %v", err)
	}
	if int(sendResp["status_code"].(float64)) != 200 {
		t.Fatalf("send status=%v", sendResp["status_code"])
	}

	exit, out, errOut = runCLI(t, stack.args("--output", "json", "templates", "save", "--id", "tpl-1", "--name", "health", "--method", "GET", "--url", upstream.URL+"/health")...)
	if exit != 0 {
		t.Fatalf("templates save exit=%d err=%s", exit, errOut)
	}
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "templates", "list")...)
	if exit != 0 {
		t.Fatalf("templates list exit=%d err=%s", exit, errOut)
	}
	var templates []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &templates); err != nil {
		t.Fatalf("templates list json: %v", err)
	}
	if len(templates) != 1 || templates[0]["id"] != "tpl-1" {
		t.Fatalf("unexpected templates list: %#v", templates)
	}
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "templates", "execute", "tpl-1")...)
	if exit != 0 {
		t.Fatalf("templates execute by id exit=%d err=%s", exit, errOut)
	}
	var execResp map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &execResp); err != nil {
		t.Fatalf("templates execute by id json: %v", err)
	}
	if int(execResp["status_code"].(float64)) != 200 || execResp["body"] != "pong" {
		t.Fatalf("unexpected execute response by id: %#v", execResp)
	}
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "templates", "execute", "health")...)
	if exit != 0 {
		t.Fatalf("templates execute by name exit=%d err=%s", exit, errOut)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &execResp); err != nil {
		t.Fatalf("templates execute by name json: %v", err)
	}
	if int(execResp["status_code"].(float64)) != 200 || execResp["body"] != "pong" {
		t.Fatalf("unexpected execute response by name: %#v", execResp)
	}
	exit, _, errOut = runCLI(t, stack.args("templates", "delete", "tpl-1")...)
	if exit != 0 {
		t.Fatalf("templates delete exit=%d err=%s", exit, errOut)
	}

	stack.storeTransaction(t, "req-replay", "GET", upstream.URL+"/replay", "", "", 0)
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "replay", "req-replay")...)
	if exit != 0 {
		t.Fatalf("replay exit=%d err=%s", exit, errOut)
	}

	stack.storeTransaction(t, "clear-1", "GET", "https://example.com/clear", "", "", 200)
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "history", "clear", "--force")...)
	if exit != 0 {
		t.Fatalf("history clear exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(out, `"cleared":true`) {
		t.Fatalf("history clear output=%s", out)
	}

	done := make(chan struct{})
	go func() {
		entry := breakpoints.NewPausedEntry("paused-1", "bp-1", mustRequest(t, "GET", "https://example.com/paused"))
		_, _ = stack.bpm.Pause(context.Background(), entry)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	exit, _, errOut = runCLI(t, stack.args("paused", "drop", "--request-id", "paused-1")...)
	if exit != 0 {
		t.Fatalf("paused drop exit=%d err=%s", exit, errOut)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("paused request not resumed")
	}
}

func TestCLIFilter_EngineBacked(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")
	stack.storeTransaction(t, "f-get-1", "GET", "https://api.example.com/users", "", `[{"id":1}]`, 200)
	stack.storeTransaction(t, "f-post-1", "POST", "https://api.example.com/orders", `{"item":"x"}`, `{"id":99}`, 201)
	stack.storeTransaction(t, "f-get-2", "GET", "https://other.example.com/health", "", "ok", 200)

	// filter by method
	exit, out, errOut := runCLI(t, stack.args("--output", "json", "filter", "--method", "post")...)
	if exit != 0 {
		t.Fatalf("filter method exit=%d err=%s", exit, errOut)
	}
	var postItems []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &postItems); err != nil {
		t.Fatalf("filter method json: %v raw=%s", err, out)
	}
	if len(postItems) != 1 || postItems[0]["id"] != "f-post-1" {
		t.Fatalf("filter method expected 1 POST item, got %#v", postItems)
	}

	// filter by url-pattern
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "filter", "--url-pattern", "api.example.com")...)
	if exit != 0 {
		t.Fatalf("filter url exit=%d err=%s", exit, errOut)
	}
	var urlItems []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &urlItems); err != nil {
		t.Fatalf("filter url json: %v", err)
	}
	if len(urlItems) != 2 {
		t.Fatalf("filter url expected 2 items, got %d: %#v", len(urlItems), urlItems)
	}

	// filter by body substring
	exit, out, errOut = runCLI(t, stack.args("--output", "json", "filter", "--body", "item")...)
	if exit != 0 {
		t.Fatalf("filter body exit=%d err=%s", exit, errOut)
	}
	var bodyItems []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &bodyItems); err != nil {
		t.Fatalf("filter body json: %v", err)
	}
	if len(bodyItems) != 1 || bodyItems[0]["id"] != "f-post-1" {
		t.Fatalf("filter body expected 1 item, got %#v", bodyItems)
	}

	// default text output (no --output flag means global default "text")
	exit, out, errOut = runCLI(t, stack.args("filter", "--method", "GET")...)
	if exit != 0 {
		t.Fatalf("filter text exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(out, "f-get-1") || !strings.Contains(out, "f-get-2") {
		t.Fatalf("filter text output missing GET items: %s", out)
	}
}

func TestCLIExport_EngineBacked(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")
	stack.storeTransaction(t, "exp-1", "GET", "https://export.example.com/a", "", "body-a", 200)
	stack.storeTransaction(t, "exp-2", "POST", "https://export.example.com/b", `{"k":"v"}`, `{"ok":true}`, 201)

	// ndjson to stdout
	exit, out, errOut := runCLI(t, stack.args("export", "--format", "ndjson")...)
	if exit != 0 {
		t.Fatalf("export ndjson exit=%d err=%s", exit, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("export ndjson expected 2 lines, got %d: %s", len(lines), out)
	}
	for _, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("export ndjson line json: %v line=%s", err, line)
		}
		if obj["id"] == nil {
			t.Fatalf("export ndjson line missing id: %s", line)
		}
	}

	// ndjson default format
	exit, out, errOut = runCLI(t, stack.args("export")...)
	if exit != 0 {
		t.Fatalf("export default exit=%d err=%s", exit, errOut)
	}
	defaultLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(defaultLines) != 2 {
		t.Fatalf("export default expected 2 lines, got %d", len(defaultLines))
	}

	// ndjson to file
	dir := t.TempDir()
	outFile := filepath.Join(dir, "traffic.ndjson")
	exit, _, errOut = runCLI(t, stack.args("export", "--format", "ndjson", "--output", outFile)...)
	if exit != 0 {
		t.Fatalf("export file exit=%d err=%s", exit, errOut)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	fileLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(fileLines) != 2 {
		t.Fatalf("export file expected 2 lines, got %d: %s", len(fileLines), string(data))
	}

	// har format
	exit, out, errOut = runCLI(t, stack.args("export", "--format", "har")...)
	if exit != 0 {
		t.Fatalf("export har exit=%d err=%s", exit, errOut)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("export har produced no output")
	}
	var harDoc map[string]any
	if err := json.Unmarshal([]byte(out), &harDoc); err != nil {
		t.Fatalf("export har json: %v raw=%s", err, out)
	}
	if harDoc["log"] == nil {
		t.Fatalf("export har missing 'log' key: %#v", harDoc)
	}
}

func TestCLIWatchWithFilters(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	done := make(chan struct{})
	var exit int
	var out, errOut string
	go func() {
		exit, out, errOut = runCLI(t, stack.args("--output", "ndjson", "watch", "--method", "POST", "--count", "1")...)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	// This GET should be filtered out (method mismatch)
	stack.storeTransaction(t, "w-get-1", "GET", "https://watch.example.com/skip", "", "", 200)
	time.Sleep(20 * time.Millisecond)
	// This POST should be delivered
	stack.storeTransaction(t, "w-post-1", "POST", "https://watch.example.com/match", `{"a":1}`, `{"b":2}`, 201)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watch with filter did not finish")
	}
	if exit != 0 {
		t.Fatalf("watch filter exit=%d err=%s", exit, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 ndjson line, got %d: %s", len(lines), out)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("watch filter json: %v", err)
	}
	if event["id"] != "w-post-1" {
		t.Fatalf("watch filter event id=%v, want w-post-1", event["id"])
	}
}

func mustRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	return req
}

func TestCLIOperability(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "secret-token")

	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca-key.pem")
	if err := os.WriteFile(certPath, []byte("cert"), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	cfgBody := fmt.Sprintf("grpc_port: \"%d\"\nca_cert_path: %q\nca_key_path: %q\nauth_token: %q\n", stack.port, certPath, keyPath, stack.token)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exit, out, errOut := runCLI(t, stack.args("--config", cfgPath, "--output", "json", "config", "show")...)
	if exit != 0 {
		t.Fatalf("config show exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(out, `"path":`) {
		t.Fatalf("config show output=%s", out)
	}

	exit, out, errOut = runCLI(t, stack.args("--config", cfgPath, "--output", "json", "cert", "status")...)
	if exit != 0 {
		t.Fatalf("cert status exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(out, `"ready":true`) {
		t.Fatalf("cert status output=%s", out)
	}

	exit, out, errOut = runCLI(t, stack.args("--config", cfgPath, "--token", stack.token, "--output", "json", "doctor")...)
	if exit != 0 {
		t.Fatalf("doctor exit=%d err=%s", exit, errOut)
	}
	if !strings.Contains(out, `"reachable":true`) {
		t.Fatalf("doctor output=%s", out)
	}

	exit, out, errOut = runCLI(t, "completion", "bash")
	if exit != 0 || !strings.Contains(out, "complete -F _apix apix") {
		t.Fatalf("completion exit=%d err=%s out=%s", exit, errOut, out)
	}
}

func TestCLIExitCodes(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "secret-token")

	exit, _, errOut := runCLI(t, "--host", stack.host, "--port", fmt.Sprintf("%d", stack.port), "status")
	if exit != 4 || !strings.Contains(errOut, "Unauthenticated") {
		t.Fatalf("expected unauthenticated exit 4, got %d err=%s", exit, errOut)
	}

	exit, _, errOut = runCLI(t, "--host", "127.0.0.1", "--port", "1", "status")
	if exit != 6 {
		t.Fatalf("expected unavailable exit 6, got %d err=%s", exit, errOut)
	}
}

func TestCLIJSONErrorEnvelope(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "secret-token")

	exit, _, errOut := runCLI(t, "--output", "json", "--host", stack.host, "--port", fmt.Sprintf("%d", stack.port), "status")
	if exit != 4 {
		t.Fatalf("expected exit 4, got %d err=%s", exit, errOut)
	}
	payload := parseErrorEnvelope(t, errOut)
	errPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error payload: %#v", payload)
	}
	if errPayload["code"] != "unauthenticated" {
		t.Fatalf("unexpected code: %#v", errPayload["code"])
	}
	if errPayload["grpc_code"] != "Unauthenticated" {
		t.Fatalf("unexpected grpc_code: %#v", errPayload["grpc_code"])
	}
	if int(errPayload["exit_code"].(float64)) != 4 {
		t.Fatalf("unexpected exit_code: %#v", errPayload["exit_code"])
	}
}

func TestCLINDJSONErrorEnvelope(t *testing.T) {
	t.Parallel()

	exit, _, errOut := runCLI(t, "--output", "ndjson", "--host", "127.0.0.1", "--port", "1", "watch", "traffic", "--count", "1")
	if exit != 6 {
		t.Fatalf("expected exit 6, got %d err=%s", exit, errOut)
	}
	payload := parseErrorEnvelope(t, errOut)
	errPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error payload: %#v", payload)
	}
	if errPayload["code"] != "unavailable" {
		t.Fatalf("unexpected code: %#v", errPayload["code"])
	}
}

func TestCLIUnknownCommandJSONError(t *testing.T) {
	t.Parallel()

	exit, _, errOut := runCLI(t, "--output", "json", "wat")
	if exit != 2 {
		t.Fatalf("expected exit 2, got %d err=%s", exit, errOut)
	}
	payload := parseErrorEnvelope(t, errOut)
	errPayload := payload["error"].(map[string]any)
	if errPayload["code"] != "invalid_argument" {
		t.Fatalf("unexpected code: %#v", errPayload["code"])
	}
	if !strings.Contains(errPayload["message"].(string), "unknown command") {
		t.Fatalf("unexpected message: %#v", errPayload["message"])
	}
}

func TestHeaderFlagsMap_SplitsOnFirstColon(t *testing.T) {
	t.Parallel()

	headers, err := headerFlags{
		"Authorization: Bearer token:abc:def",
		"X-Time: 12:34:56",
	}.Map()
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if got := headers["Authorization"]; got != "Bearer token:abc:def" {
		t.Fatalf("Authorization header = %q", got)
	}
	if got := headers["X-Time"]; got != "12:34:56" {
		t.Fatalf("X-Time header = %q", got)
	}
}

func TestHeaderFlagsMap_InvalidHeader(t *testing.T) {
	t.Parallel()

	_, err := headerFlags{"not-a-header"}.Map()
	if err == nil {
		t.Fatal("expected parse error for malformed header")
	}
}

func TestCLIWatchJSONOutputRejected(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, _, errOut := runCLI(t, stack.args("--output", "json", "watch", "traffic", "--count", "1")...)
	if exit != 2 {
		t.Fatalf("expected invalid-argument exit=2, got %d err=%s", exit, errOut)
	}
	payload := parseErrorEnvelope(t, errOut)
	errPayload := payload["error"].(map[string]any)
	if errPayload["code"] != "invalid_argument" {
		t.Fatalf("unexpected code: %#v", errPayload["code"])
	}
	if !strings.Contains(errPayload["message"].(string), "--output ndjson") {
		t.Fatalf("unexpected message: %#v", errPayload["message"])
	}
}

func TestCLIPausedWatchJSONOutputRejected(t *testing.T) {
	t.Parallel()
	stack := newCLITestStack(t, "")

	exit, _, errOut := runCLI(t, stack.args("--output", "json", "paused", "watch", "--count", "1")...)
	if exit != 2 {
		t.Fatalf("expected invalid-argument exit=2, got %d err=%s", exit, errOut)
	}
	payload := parseErrorEnvelope(t, errOut)
	errPayload := payload["error"].(map[string]any)
	if errPayload["code"] != "invalid_argument" {
		t.Fatalf("unexpected code: %#v", errPayload["code"])
	}
	if !strings.Contains(errPayload["message"].(string), "--output ndjson") {
		t.Fatalf("unexpected message: %#v", errPayload["message"])
	}
}
