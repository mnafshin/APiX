package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mnafshin/apix/internal/breakpoints"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	"github.com/mnafshin/apix/internal/pluginrt"
	"github.com/mnafshin/apix/internal/replay"
	"github.com/mnafshin/apix/internal/storage"
	"github.com/mnafshin/apix/pkg/version"
)

func newMCPFixture(t *testing.T, cfg *config.Config) (*storage.DB, *httptest.Server) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	eng := engine.New(db, breakpoints.NewManager(), pluginrt.NewRuntime())
	re := replay.NewEngine(db, nil)
	srv := httptest.NewServer(newMCPHandler(NewEngineServer(eng, re, cfg), cfg))
	t.Cleanup(func() {
		srv.Close()
		_ = db.Close()
	})
	return db, srv
}

func rpcCall(t *testing.T, client *http.Client, url, token, method string, params map[string]interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http call: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode response: %v", err)
	}
	_ = resp.Body.Close()
	return resp, decoded
}

func TestMCPInitializeAndToolsList(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		HTTPPort:       "8080",
		GRPCPort:       "9090",
		MCPEnabled:     true,
		MCPAllowReplay: true,
	}
	_, ts := newMCPFixture(t, cfg)
	client := ts.Client()

	_, initResp := rpcCall(t, client, ts.URL, "", mcpMethodInitialize, map[string]interface{}{})
	result, ok := initResp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("initialize result missing: %#v", initResp)
	}
	serverInfo, _ := result["serverInfo"].(map[string]interface{})
	if serverInfo["version"] != version.Version {
		t.Fatalf("unexpected version: got %v want %v", serverInfo["version"], version.Version)
	}

	_, toolsResp := rpcCall(t, client, ts.URL, "", mcpMethodToolsList, map[string]interface{}{})
	toolsResult, _ := toolsResp["result"].(map[string]interface{})
	tools, _ := toolsResult["tools"].([]interface{})
	if len(tools) < 2 {
		t.Fatalf("expected at least 2 tools, got %d", len(tools))
	}
}

func TestMCPAuthTokenRequired(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		HTTPPort:       "8080",
		GRPCPort:       "9090",
		MCPEnabled:     true,
		AuthToken:      "secret",
		MCPBindAddress: "127.0.0.1",
	}
	_, ts := newMCPFixture(t, cfg)
	client := ts.Client()

	resp, body := rpcCall(t, client, ts.URL, "", mcpMethodToolsList, map[string]interface{}{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d body=%v", resp.StatusCode, body)
	}
	resp, body = rpcCall(t, client, ts.URL, "secret", mcpMethodToolsList, map[string]interface{}{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected success with token, got %d body=%v", resp.StatusCode, body)
	}
}

func TestMCPHistoryQueryTool(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{HTTPPort: "8080", GRPCPort: "9090", MCPEnabled: true}
	db, ts := newMCPFixture(t, cfg)
	client := ts.Client()

	reqRecord := &storage.RequestRecord{
		ID:        "hist-1",
		Method:    "GET",
		URL:       "https://example.com/api",
		Headers:   map[string]string{"Accept": "application/json"},
		Body:      []byte("request-body"),
		Timestamp: time.Now(),
	}
	if err := db.SaveRequest(reqRecord); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	if err := db.SaveResponse(&storage.ResponseRecord{
		RequestID:  reqRecord.ID,
		StatusCode: http.StatusOK,
		StatusText: "200 OK",
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatalf("SaveResponse: %v", err)
	}

	_, body := rpcCall(t, client, ts.URL, "", mcpMethodToolsCall, map[string]interface{}{
		"name": "apix.history.query",
		"arguments": map[string]interface{}{
			"limit":      10,
			"url_filter": "example.com",
		},
	})
	result, _ := body["result"].(map[string]interface{})
	structured, _ := result["structuredContent"].(map[string]interface{})
	count, _ := structured["count"].(float64)
	if int(count) != 1 {
		t.Fatalf("expected count=1, got %v", structured["count"])
	}
}

func TestMCPComposeToolDisabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{HTTPPort: "8080", GRPCPort: "9090", MCPEnabled: true}
	_, ts := newMCPFixture(t, cfg)
	client := ts.Client()

	_, body := rpcCall(t, client, ts.URL, "", mcpMethodToolsCall, map[string]interface{}{
		"name": "apix.compose.request",
		"arguments": map[string]interface{}{
			"method": "GET",
			"url":    "https://example.com",
		},
	})
	if _, ok := body["error"]; !ok {
		t.Fatalf("expected tool-disabled error, got: %#v", body)
	}
}

func TestMCPComposeToolExecutesWhenEnabled(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok-compose"))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		HTTPPort:        "8080",
		GRPCPort:        "9090",
		MCPEnabled:      true,
		MCPAllowCompose: true,
	}
	_, ts := newMCPFixture(t, cfg)
	client := ts.Client()

	_, body := rpcCall(t, client, ts.URL, "", mcpMethodToolsCall, map[string]interface{}{
		"name": "apix.compose.request",
		"arguments": map[string]interface{}{
			"method": "GET",
			"url":    upstream.URL,
		},
	})
	result, _ := body["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("expected result, got %#v", body)
	}
	structured, _ := result["structuredContent"].(map[string]interface{})
	if structured["status_code"] != float64(http.StatusOK) {
		t.Fatalf("unexpected status_code: %v", structured["status_code"])
	}
}

func TestStartMCPServerDisabledNoop(t *testing.T) {
	t.Parallel()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	eng := engine.New(db, breakpoints.NewManager(), pluginrt.NewRuntime())
	re := replay.NewEngine(db, nil)
	cfg := &config.Config{MCPEnabled: false}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		StartMCPServer(ctx, eng, re, cfg)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("expected StartMCPServer to return immediately when disabled")
	}
}
