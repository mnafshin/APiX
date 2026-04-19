package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	metrics "github.com/mnafshin/apix/internal/metrics"
	"github.com/mnafshin/apix/internal/rewrite"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mnafshin/apix/internal/config"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/pkg/plugins"
)

// HTTPProxy is a forward proxy that intercepts plain HTTP traffic and tunnels
// HTTPS CONNECT requests to the MITM TLS proxy.
type HTTPProxy struct {
	addr      string
	tlsProxy  *TLSProxy
	plugins   any
	engine    TrafficEngine
	transport *http.Transport
	cfg       *config.Config
}

// NewHTTPProxy creates a new HTTP proxy listening on addr.
func NewHTTPProxy(addr string, tlsProxy *TLSProxy, engine TrafficEngine, opts TransportOptions, cfg *config.Config) *HTTPProxy {
	return &HTTPProxy{
		addr:      addr,
		tlsProxy:  tlsProxy,
		engine:    engine,
		transport: newTransport(nil, opts),
		cfg:       cfg,
	}
}

// Start begins accepting connections. Blocks until ctx is cancelled.
func (p *HTTPProxy) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              p.addr,
		Handler:           p,
		ReadHeaderTimeout: time.Duration(p.cfg.HTTPReadHeaderTimeout) * time.Second,
		ReadTimeout:       time.Duration(p.cfg.HTTPReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(p.cfg.HTTPWriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(p.cfg.HTTPIdleTimeout) * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logging.Errorf(ctx, "http proxy shutdown: %v", err)
		}
	}()
	logging.Infof(ctx, "HTTP proxy listening on %s (timeouts: header=%v, read=%v, write=%v, idle=%v)",
		p.addr, srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http proxy: %w", err)
	}
	return nil
}

// ServeHTTP handles both plain HTTP and CONNECT (tunnel) requests.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "HTTP proxy panic in ServeHTTP (recovered): %v", rec)
			http.Error(w, "proxy error", http.StatusBadGateway)
		}
	}()

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleConnect upgrades the connection for HTTPS tunnelling.
func (p *HTTPProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "HTTP proxy panic in handleConnect (recovered): %v", rec)
			http.Error(w, "proxy error", http.StatusInternalServerError)
		}
	}()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		logging.Errorf(ctx, "hijack: %v", err)
		return
	}

	// Flush any buffered data from the hijacked reader.
	if brw.Reader.Buffered() > 0 {
		_ = conn.Close()
		logging.Warnf(ctx, "handleConnect: unexpected buffered data after hijack")
		return
	}

	// Respond 200 Connection established.
	_, err = fmt.Fprint(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
	if err != nil {
		_ = conn.Close()
		return
	}

	proto, err := brw.Peek(1)
	if err != nil {
		_ = conn.Close()
		logging.Errorf(ctx, "peek CONNECT tunnel protocol: %v", err)
		return
	}
	if len(proto) > 0 && proto[0] == 0x16 {
		p.tlsProxy.handleBufferedConn(ctx, conn, r.Host, brw.Reader)
		return
	}
	p.handleTunnelConn(ctx, conn, brw.Reader, r.Host)
}

// handleHTTP proxies a plain HTTP request, runs plugin chain, stores traffic.
func (p *HTTPProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reqID := logging.EnsureRequestID(r.Header)
	ctx = logging.WithRequestID(ctx, reqID)

	// Instrument active connections
	metrics.IncActive()
	defer metrics.DecActive()

	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "HTTP proxy panic (recovered): %v", rec)
			http.Error(w, "proxy error", http.StatusBadGateway)
		}
	}()

	start := time.Now()
	maxBodyBytes := int64(p.cfg.MaxBodySizeMB) * 1024 * 1024

	// 1. Buffer request body.
	bodyBytes, err := p.readRequestBody(r, maxBodyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("request body too large: %v", err), http.StatusRequestEntityTooLarge)
		return
	}

	origHeaders := r.Header.Clone()
	protocol := r.Proto
	if protocol == "" {
		protocol = "HTTP/1.1"
	}

	proxyReq := &plugins.ProxyRequest{
		ID:       reqID,
		Method:   r.Method,
		URL:      r.URL,
		Headers:  r.Header.Clone(),
		Body:     io.NopCloser(bytes.NewReader(bodyBytes)),
		Protocol: protocol,
		Raw:      r,
	}

	// 2. Run plugin OnRequest chain.
	proxyReq, err = p.runPluginRequest(ctx, proxyReq)
	if err != nil {
		http.Error(w, "plugin error", http.StatusBadGateway)
		return
	}

	// 3. Short-circuit if plugin provided a mocked response.
	if proxyReq.MockedResponse != nil {
		p.writeMockedResponse(ctx, w, proxyReq.MockedResponse)
		return
	}

	// 4. Evaluate breakpoints (pause/resume/drop).
	tx := &Transaction{
		ID:                     reqID,
		Request:                proxyReq,
		RequestBody:            bodyBytes,
		OriginalRequestHeaders: origHeaders,
	}
	tx, done, err := p.evaluateBreakpoint(ctx, w, tx)
	if err != nil || done {
		return
	}

	// 5. Route WebSocket upgrades to dedicated handler.
	if isWebSocketRequest(r) {
		p.handleWebSocket(ctx, w, r, tx, start)
		return
	}

	// 6. Apply request rewrite rules.
	if sent := p.applyRequestRewriteRules(ctx, w, r, bodyBytes); sent {
		return
	}

	// 7. Forward request upstream and read response.
	upResp, respBody, err := p.forwardUpstream(ctx, proxyReq, maxBodyBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	proxyResp := &plugins.ProxyResponse{
		StatusCode: upResp.StatusCode,
		Status:     upResp.Status,
		Headers:    upResp.Header.Clone(),
		Body:       nil,
		Raw:        upResp,
	}

	// 8. Apply response rewrite rules.
	respBody = p.applyResponseRewriteRules(ctx, r, upResp, proxyResp, respBody)

	// 9. Run plugin OnResponse chain.
	proxyResp, respBody, err = p.runPluginResponse(ctx, proxyReq, proxyResp, respBody, maxBodyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("plugin OnResponse error: %v", err), http.StatusBadGateway)
		return
	}

	// 10. Store transaction.
	if p.engine != nil {
		tx.Response = proxyResp
		tx.ResponseBody = respBody
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "store transaction: %v", err)
		}
	}

	// 11. Emit metrics + slowlog.
	durationSec := time.Since(start).Seconds()
	metrics.ObserveRequest(proxyReq.Method, proxyResp.StatusCode, durationSec)
	if p.cfg != nil && p.cfg.SlowlogThresholdMs > 0 {
		if time.Since(start).Milliseconds() > int64(p.cfg.SlowlogThresholdMs) {
			logging.Warnf(ctx, "slow request: method=%s url=%s status=%d duration_ms=%d request_id=%s",
				proxyReq.Method, proxyReq.URL.String(), proxyResp.StatusCode, time.Since(start).Milliseconds(), reqID)
		}
	}

	writeProxyResponse(w, proxyResp, respBody)
}

// readRequestBody buffers r.Body up to maxBytes.
func (p *HTTPProxy) readRequestBody(r *http.Request, maxBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := httputil.ReadLimitedBody(r.Body, maxBytes)
	_ = r.Body.Close()
	return b, err
}

// runPluginRequest runs the OnRequest plugin chain with panic recovery.
// Returns the (possibly modified) ProxyRequest, or an error on failure.
func (p *HTTPProxy) runPluginRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	return runPluginRequest(ctx, p.plugins, req, "http proxy")
}

// writeMockedResponse writes a plugin-provided mocked response to w.
func (p *HTTPProxy) writeMockedResponse(ctx context.Context, w http.ResponseWriter, mock *plugins.ProxyResponse) {
	var body []byte
	if mock.Body != nil {
		var err error
		body, err = io.ReadAll(mock.Body)
		if err != nil {
			logging.Errorf(ctx, "read mocked response body: %v", err)
		}
	}
	writeProxyResponse(w, mock, body)
}

// evaluateBreakpoint checks breakpoints on the transaction.
// Returns (modified tx, true if response was written, error).
func (p *HTTPProxy) evaluateBreakpoint(ctx context.Context, w http.ResponseWriter, tx *Transaction) (*Transaction, bool, error) {
	if p.engine == nil {
		return tx, false, nil
	}
	modified, action, err := p.engine.PauseRequest(tx)
	if err != nil {
		logging.Errorf(ctx, "pause request: %v", err)
		http.Error(w, fmt.Sprintf("pause request: %v", err), http.StatusBadGateway)
		return tx, true, err
	}
	tx = modified
	switch action {
	case ResumeDrop:
		http.Error(w, "request dropped by breakpoint", http.StatusBadGateway)
		return tx, true, nil
	case ResumeRespond:
		if tx.Response != nil {
			var respBody []byte
			if tx.Response.Body != nil {
				var readErr error
				respBody, readErr = io.ReadAll(tx.Response.Body)
				if readErr != nil {
					logging.Errorf(ctx, "read synthetic response body: %v", readErr)
				}
			}
			writeProxyResponse(w, tx.Response, respBody)
		} else {
			http.Error(w, "no synthetic response provided", http.StatusBadGateway)
		}
		return tx, true, nil
	}
	return tx, false, nil
}

// applyRequestRewriteRules applies matching rewrite rules to the outbound request.
// Returns true if a synthetic response was written and the caller should return immediately.
func (p *HTTPProxy) applyRequestRewriteRules(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte) bool {
	if p.engine == nil {
		return false
	}
	rules, err := p.engine.RewriteRules()
	if err != nil {
		logging.Errorf(ctx, "load rewrite rules: %v", err)
		return false
	}
	if len(rules) == 0 {
		return false
	}
	synth, err := rewrite.ApplyRequestRules(rules, r, body)
	if err != nil {
		logging.Errorf(ctx, "apply rewrite rules: %v", err)
		return false
	}
	if synth == nil {
		return false
	}
	for k, vv := range synth.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(synth.StatusCode)
	if len(synth.Body) > 0 {
		_, _ = w.Write(synth.Body) //nolint:gosec // G705: proxy intentionally echoes response body to client
	}
	return true
}

// forwardUpstream sends the request upstream and returns the raw response plus buffered body.
func (p *HTTPProxy) forwardUpstream(ctx context.Context, proxyReq *plugins.ProxyRequest, maxBodyBytes int64) (*http.Response, []byte, error) {
	upReq, err := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("build upstream request: %w", err)
	}
	upReq.Header = proxyReq.Headers

	upResp, err := p.transport.RoundTrip(upReq)
	if err != nil {
		return nil, nil, fmt.Errorf("upstream error: %w", err)
	}

	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		_ = upResp.Body.Close()
		return nil, nil, fmt.Errorf("response body read error: %w", err)
	}
	if int64(len(respBody)) > maxBodyBytes {
		_ = upResp.Body.Close()
		return nil, nil, fmt.Errorf("response body too large: %d bytes > %d bytes", len(respBody), maxBodyBytes)
	}
	return upResp, respBody, nil
}

// applyResponseRewriteRules applies matching rewrite rules to the upstream response.
// Returns the (possibly modified) response body.
func (p *HTTPProxy) applyResponseRewriteRules(ctx context.Context, r *http.Request, upResp *http.Response, proxyResp *plugins.ProxyResponse, body []byte) []byte {
	if p.engine == nil {
		return body
	}
	rules, err := p.engine.RewriteRules()
	if err != nil {
		logging.Errorf(ctx, "load rewrite rules (response): %v", err)
		return body
	}
	if len(rules) == 0 {
		return body
	}
	body = rewrite.ApplyResponseRules(rules, r, upResp, body)
	proxyResp.Headers = upResp.Header.Clone()
	return body
}

// runPluginResponse runs the OnResponse plugin chain with panic recovery.
// Returns the (possibly modified) ProxyResponse, final body bytes, and any error.
func (p *HTTPProxy) runPluginResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse, body []byte, maxBodyBytes int64) (*plugins.ProxyResponse, []byte, error) {
	if p.plugins == nil {
		return resp, body, nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	modResp := runPluginResponse(ctx, p.plugins, req, resp, "http proxy")
	if modResp == nil {
		return resp, body, nil
	}
	if modResp.Body != nil {
		newBody := drainPluginResponseBody(ctx, modResp, maxBodyBytes, "http proxy")
		return modResp, newBody, nil
	}
	return modResp, body, nil
}

func (p *HTTPProxy) handleWebSocket(ctx context.Context, w http.ResponseWriter, r *http.Request, tx *Transaction, start time.Time) {
	upstreamHeaders := copyWebSocketRequestHeaders(tx.Request.Headers)
	dialer := newWebSocketDialer(p.transport, tx.Request)
	upstreamConn, resp, err := dialer.DialContext(ctx, webSocketTargetURL(tx.Request.URL), upstreamHeaders)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
			msg, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
			if readErr == nil && len(msg) > 0 {
				http.Error(w, fmt.Sprintf("websocket upstream error: %v: %s", err, msg), http.StatusBadGateway)
				return
			}
		}
		http.Error(w, fmt.Sprintf("websocket upstream error: %v", err), http.StatusBadGateway)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	if protocol := upstreamConn.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	clientConn, err := upgrader.Upgrade(w, r, copyWebSocketResponseHeaders(resp.Header))
	if err != nil {
		_ = upstreamConn.Close()
		logging.Errorf(ctx, "websocket client upgrade: %v", err)
		return
	}

	tx.Response = &plugins.ProxyResponse{
		StatusCode: http.StatusSwitchingProtocols,
		Status:     resp.Status,
		Headers:    resp.Header.Clone(),
	}
	tx.DurationMs = time.Since(start).Milliseconds()
	if p.engine != nil {
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "store websocket upgrade transaction: %v", err)
		}
	}

	relayWebSocket(ctx, p.engine, tx.ID, clientConn, upstreamConn)
}

func (p *HTTPProxy) handleTunnelConn(ctx context.Context, conn net.Conn, br *bufio.Reader, host string) {
	defer func() { _ = conn.Close() }()

	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			if err != io.EOF {
				logging.Errorf(ctx, "read tunneled request from %s: %v", host, err)
			}
			return
		}
		if req.URL.Host == "" {
			req.URL.Host = host
		}
		if req.URL.Scheme == "" {
			req.URL.Scheme = "http"
		}
		req = req.WithContext(ctx)
		w := newHijackableResponseWriter(conn, br)
		p.handleHTTP(w, req)
		if isWebSocketRequest(req) {
			return
		}
	}
}

// writeProxyResponse writes a ProxyResponse back to the http.ResponseWriter.
func writeProxyResponse(w http.ResponseWriter, resp *plugins.ProxyResponse, body []byte) {
	for k, vv := range resp.Headers {
		// Don't copy Content-Length; we'll set it accurately based on body
		if k == "Content-Length" {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	if len(body) > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	} else {
		w.Header().Set("Content-Length", "0")
	}

	w.WriteHeader(resp.StatusCode)
	if len(body) > 0 {
		if _, err := w.Write(body); err != nil { //nolint:gosec // G705: proxy intentionally writes upstream response body to client
			rid := resp.Headers.Get(logging.RequestIDHeader)
			ctx := context.Background()
			if rid != "" {
				ctx = logging.WithRequestID(ctx, rid)
			}
			logging.Errorf(ctx, "proxy: write response to client: %v", err)
		}
	}
}

// SetPlugins wires the plugin chain into the HTTP proxy. chain may implement
// RequestPlugin, ResponsePlugin, or both (PluginChain). Passing nil disables
// plugin execution.
func (p *HTTPProxy) SetPlugins(chain any) {
	p.plugins = chain
}

// Close gracefully shuts down the HTTP proxy and closes idle connections.
func (p *HTTPProxy) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}
