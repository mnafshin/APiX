package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/internal/engine"
	logging "github.com/mnafshin/apix/internal/logging"
	"github.com/mnafshin/apix/internal/replay"
	apix "github.com/mnafshin/apix/pkg/api/generated"
	"github.com/mnafshin/apix/pkg/version"
)

const (
	mcpMethodInitialize = "initialize"
	mcpMethodToolsList  = "tools/list"
	mcpMethodToolsCall  = "tools/call"
)

type mcpHandler struct {
	srv *EngineServer
	cfg *config.Config
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type mcpToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// NewMCPHandler builds the MCP HTTP handler. It exposes a JSON-RPC MCP endpoint
// at /mcp and reuses the same engine + replay components used by the gRPC API.
func NewMCPHandler(eng *engine.Engine, re *replay.Engine, cfg *config.Config) http.Handler {
	return newMCPHandler(NewEngineServer(eng, re, cfg), cfg)
}

func newMCPHandler(s *EngineServer, cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	h := &mcpHandler{srv: s, cfg: cfg}
	mux.HandleFunc("/mcp", h.handle)
	return mux
}

// StartMCPServer starts the MCP HTTP server and blocks until it exits.
func StartMCPServer(ctx context.Context, eng *engine.Engine, re *replay.Engine, cfg *config.Config) {
	if !cfg.MCPEnabled {
		return
	}
	addr := cfg.MCPBindAddress + ":" + cfg.MCPPort
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewMCPHandler(eng, re, cfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logging.Warnf(ctx, "MCP server shutdown: %v", err)
		}
	}()

	logging.Infof(ctx, "MCP server listening on %s (TLS=%t)", addr, cfg.TLSEnabled)
	var err error
	if cfg.TLSEnabled {
		err = srv.ListenAndServeTLS(cfg.GRPCCertPath, cfg.GRPCKeyPath)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		logging.Errorf(ctx, "MCP server error: %v", err)
	}
}

func (h *mcpHandler) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.isAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"error": map[string]interface{}{
				"code":    "unauthorized",
				"message": "missing or invalid bearer token",
			},
		})
		return
	}

	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error: &mcpError{
				Code:    -32700,
				Message: "parse error",
				Data:    err.Error(),
			},
		})
		return
	}

	if len(req.ID) == 0 {
		req.ID = json.RawMessage("null")
	}
	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
	result, rpcErr := h.route(r.Context(), req)
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *mcpHandler) route(ctx context.Context, req mcpRequest) (interface{}, *mcpError) {
	switch req.Method {
	case mcpMethodInitialize:
		return map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "apix-mcp",
				"version": version.Version,
			},
		}, nil
	case mcpMethodToolsList:
		return map[string]interface{}{
			"tools": h.tools(),
		}, nil
	case mcpMethodToolsCall:
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &mcpError{Code: -32602, Message: "invalid params", Data: err.Error()}
		}
		return h.callTool(ctx, params)
	default:
		return nil, &mcpError{
			Code:    -32601,
			Message: "method not found",
			Data:    map[string]interface{}{"method": req.Method},
		}
	}
}

func (h *mcpHandler) tools() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "apix.status",
			Description: "Return APiX engine status, version, and listening ports.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "apix.history.query",
			Description: "Query captured APiX traffic history with filters.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit":         map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 1000},
					"offset":        map[string]interface{}{"type": "integer", "minimum": 0},
					"url_filter":    map[string]interface{}{"type": "string"},
					"method_filter": map[string]interface{}{"type": "string"},
					"status_filter": map[string]interface{}{"type": "integer"},
					"since_ms":      map[string]interface{}{"type": "integer"},
					"body_filter":   map[string]interface{}{"type": "string"},
				},
			},
		},
	}
	if h.cfg.MCPAllowReplay {
		tools = append(tools, mcpTool{
			Name:        "apix.replay.request",
			Description: "Replay a previously captured request by request_id (network side effect).",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"request_id"},
				"properties": map[string]interface{}{
					"request_id":       map[string]interface{}{"type": "string"},
					"follow_redirects": map[string]interface{}{"type": "boolean"},
					"override_headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
					"override_body":    map[string]interface{}{"type": "string"},
				},
			},
		})
	}
	if h.cfg.MCPAllowCompose {
		tools = append(tools, mcpTool{
			Name:        "apix.compose.request",
			Description: "Compose and execute an ad-hoc HTTP request (network side effect).",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []string{"method", "url"},
				"properties": map[string]interface{}{
					"method":           map[string]interface{}{"type": "string"},
					"url":              map[string]interface{}{"type": "string"},
					"headers":          map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
					"body":             map[string]interface{}{"type": "string"},
					"follow_redirects": map[string]interface{}{"type": "boolean"},
				},
			},
		})
	}
	return tools
}

func (h *mcpHandler) callTool(ctx context.Context, params mcpToolCallParams) (interface{}, *mcpError) {
	args := params.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	var (
		payload interface{}
		err     error
	)
	switch params.Name {
	case "apix.status":
		payload, err = h.callStatus(ctx)
	case "apix.history.query":
		payload, err = h.callHistory(args)
	case "apix.replay.request":
		if !h.cfg.MCPAllowReplay {
			return nil, &mcpError{Code: -32602, Message: "tool disabled", Data: "mcp_allow_replay=false"}
		}
		payload, err = h.callReplay(ctx, args)
	case "apix.compose.request":
		if !h.cfg.MCPAllowCompose {
			return nil, &mcpError{Code: -32602, Message: "tool disabled", Data: "mcp_allow_compose=false"}
		}
		payload, err = h.callCompose(ctx, args)
	default:
		return nil, &mcpError{Code: -32601, Message: "tool not found", Data: map[string]interface{}{"name": params.Name}}
	}
	if err != nil {
		return nil, &mcpError{Code: -32603, Message: "tool execution failed", Data: err.Error()}
	}

	textPayload, _ := json.Marshal(payload)
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": string(textPayload)},
		},
		"structuredContent": payload,
	}, nil
}

func (h *mcpHandler) callStatus(ctx context.Context) (interface{}, error) {
	statusResp, err := h.srv.GetStatus(ctx, &apix.StatusRequest{})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":      statusResp.Status,
		"version":     statusResp.Version,
		"proxy_port":  statusResp.ProxyPort,
		"grpc_port":   statusResp.GrpcPort,
		"tls_enabled": statusResp.TlsEnabled,
	}, nil
}

func (h *mcpHandler) callHistory(args map[string]interface{}) (interface{}, error) {
	limit := intArg(args, "limit", 100)
	offset := intArg(args, "offset", 0)
	urlFilter := stringArg(args, "url_filter")
	methodFilter := stringArg(args, "method_filter")
	statusFilter := intArg(args, "status_filter", 0)
	sinceMS := int64Arg(args, "since_ms", 0)
	bodyFilter := stringArg(args, "body_filter")

	reqs, resps, err := h.srv.engine.ListTransactions(limit, offset, urlFilter, methodFilter, statusFilter, bodyFilter)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(reqs))
	for i, r := range reqs {
		if sinceMS > 0 && r.Timestamp.UnixMilli() < sinceMS {
			continue
		}
		tx := map[string]interface{}{
			"id":           r.ID,
			"method":       r.Method,
			"url":          r.URL,
			"headers":      r.Headers,
			"body":         string(r.Body),
			"timestamp_ms": r.Timestamp.UnixMilli(),
			"duration_ms":  r.DurationMs,
			"protocol":     r.Protocol,
		}
		if i < len(resps) && resps[i] != nil {
			tx["response"] = map[string]interface{}{
				"status_code": resps[i].StatusCode,
				"status_text": resps[i].StatusText,
				"headers":     resps[i].Headers,
				"body":        string(resps[i].Body),
			}
		}
		out = append(out, tx)
	}
	return map[string]interface{}{"transactions": out, "count": len(out)}, nil
}

func (h *mcpHandler) callReplay(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	requestID := stringArg(args, "request_id")
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	spec := &apix.ReplaySpec{
		Source:          &apix.ReplaySpec_RequestId{RequestId: requestID},
		FollowRedirects: boolArg(args, "follow_redirects", false),
		OverrideHeaders: mapArg(args, "override_headers"),
		OverrideBody:    []byte(stringArg(args, "override_body")),
	}
	resp, err := h.srv.ReplayRequest(ctx, spec)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"status_text": resp.StatusText,
		"headers":     resp.Headers,
		"body":        string(resp.Body),
	}, nil
}

func (h *mcpHandler) callCompose(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	method := stringArg(args, "method")
	rawURL := stringArg(args, "url")
	if method == "" || rawURL == "" {
		return nil, fmt.Errorf("method and url are required")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, io.NopCloser(bytes.NewReader([]byte(stringArg(args, "body")))))
	if err != nil {
		return nil, err
	}
	for k, v := range mapArg(args, "headers") {
		req.Header.Set(k, v)
	}
	spec := &apix.ReplaySpec{
		Source:          &apix.ReplaySpec_RawRequest{RawRequest: &apix.HttpRequest{Method: req.Method, Url: req.URL.String(), Headers: headersToMap(req.Header)}},
		FollowRedirects: boolArg(args, "follow_redirects", false),
		OverrideBody:    []byte(stringArg(args, "body")),
	}
	resp, err := h.srv.ReplayRequest(ctx, spec)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"status_text": resp.StatusText,
		"headers":     resp.Headers,
		"body":        string(resp.Body),
	}, nil
}

func (h *mcpHandler) isAuthorized(r *http.Request) bool {
	if h.cfg.AuthToken == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+h.cfg.AuthToken
}

func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func int64Arg(args map[string]interface{}, key string, fallback int64) int64 {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	raw, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func mapArg(args map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	if args == nil {
		return out
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return out
	}
	switch typed := raw.(type) {
	case map[string]interface{}:
		for k, v := range typed {
			out[k] = stringArg(map[string]interface{}{"v": v}, "v")
		}
	case map[string]string:
		for k, v := range typed {
			out[k] = v
		}
	}
	return out
}

func headersToMap(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vals := range headers {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}
