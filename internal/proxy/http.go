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
	plugins   PluginChain
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

	// Buffer the entire request body so it can be stored and forwarded.
	// Limit body size to prevent OOM denial of service.
	maxBodyBytes := int64(p.cfg.MaxBodySizeMB) * 1024 * 1024
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = httputil.ReadLimitedBody(r.Body, maxBodyBytes)
		if err != nil {
			http.Error(w, fmt.Sprintf("request body too large: %v", err), http.StatusRequestEntityTooLarge)
			return
		}
		_ = r.Body.Close()
	}

	origHeaders := r.Header.Clone()

	// Detect negotiated protocol. For HTTP/1.x requests r.Proto is "HTTP/1.1".
	// For h2c (cleartext HTTP/2) the Go standard library upgrades the connection
	// before reaching the handler, so r.Proto will be "HTTP/2.0".
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

	// Run plugin OnRequest chain (with panic recovery).
	var pluginReqFailed bool
	if p.plugins != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logging.Errorf(ctx, "HTTP proxy panic in plugin OnRequest (recovered): %v", rec)
					pluginReqFailed = true
				}
			}()
			modified, err := p.plugins.RunRequest(ctx, proxyReq)
			if err != nil {
				logging.Errorf(ctx, "plugin OnRequest error: %v", err)
				pluginReqFailed = true
				return
			}
			proxyReq = modified
		}()
		if pluginReqFailed {
			http.Error(w, "plugin error", http.StatusBadGateway)
			return
		}
	}

	// If a plugin set a mocked response, short-circuit.
	if proxyReq.MockedResponse != nil {
		var mockedBody []byte
		if proxyReq.MockedResponse.Body != nil {
			var err error
			mockedBody, err = io.ReadAll(proxyReq.MockedResponse.Body)
			if err != nil {
				logging.Errorf(ctx, "read mocked response body: %v", err)
			}
		}
		writeProxyResponse(w, proxyReq.MockedResponse, mockedBody)
		return
	}

	// Check breakpoints.
	tx := &Transaction{
		ID:                     reqID,
		Request:                proxyReq,
		RequestBody:            bodyBytes,
		OriginalRequestHeaders: origHeaders,
	}
	if p.engine != nil {
		bpID := "" // The engine handles evaluation internally via PauseRequest.
		_ = bpID
		modified, action, err := p.engine.PauseRequest(tx)
		if err != nil {
			logging.Errorf(ctx, "pause request: %v", err)
			http.Error(w, fmt.Sprintf("pause request: %v", err), http.StatusBadGateway)
			return
		}
		tx = modified
		switch action {
		case ResumeDrop:
			http.Error(w, "request dropped by breakpoint", http.StatusBadGateway)
			return
		case ResumeRespond:
			if tx.Response != nil {
				var respBody []byte
				if tx.Response.Body != nil {
					var err error
					respBody, err = io.ReadAll(tx.Response.Body)
					if err != nil {
						logging.Errorf(ctx, "read synthetic response body: %v", err)
					}
				}
				writeProxyResponse(w, tx.Response, respBody)
			} else {
				http.Error(w, "no synthetic response provided", http.StatusBadGateway)
			}
			return
		}
	}

	if isWebSocketRequest(r) {
		p.handleWebSocket(ctx, w, r, tx, start)
		return
	}

	// Apply rewrite rules to the request (before forwarding upstream).
	if p.engine != nil {
		rules, err := p.engine.RewriteRules()
		if err != nil {
			logging.Errorf(ctx, "load rewrite rules: %v", err)
		} else if len(rules) > 0 {
			synth, err := rewrite.ApplyRequestRules(rules, r, bodyBytes)
			if err != nil {
				logging.Errorf(ctx, "apply rewrite rules: %v", err)
			} else if synth != nil {
				for k, vv := range synth.Headers {
					for _, v := range vv {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(synth.StatusCode)
				if len(synth.Body) > 0 {
					_, _ = w.Write(synth.Body)
				}
				return
			}
		}
	}

	// Build upstream request.
	upReq, err := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("build upstream request: %v", err), http.StatusBadGateway)
		return
	}
	upReq.Header = proxyReq.Headers

	upResp, err := p.transport.RoundTrip(upReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	// Limit response body size to prevent OOM denial of service.
	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("response body read error: %v", err), http.StatusBadGateway)
		return
	}

	// Enforce size limit after reading
	if int64(len(respBody)) > maxBodyBytes {
		http.Error(w, fmt.Sprintf("response body too large: %d bytes > %d bytes", len(respBody), maxBodyBytes), http.StatusBadGateway)
		return
	}

	// Create proxyResp without Body initially - keep respBody buffered separately.
	// After plugins run, we'll use the final body bytes directly in writeProxyResponse.
	proxyResp := &plugins.ProxyResponse{
		StatusCode: upResp.StatusCode,
		Status:     upResp.Status,
		Headers:    upResp.Header.Clone(),
		Body:       nil,
		Raw:        upResp,
	}

	// Apply response rewrite rules before plugins.
	if p.engine != nil {
		rules, err := p.engine.RewriteRules()
		if err != nil {
			logging.Errorf(ctx, "load rewrite rules (response): %v", err)
		} else if len(rules) > 0 {
			respBody = rewrite.ApplyResponseRules(rules, r, upResp, respBody)
			// Sync updated headers back into proxyResp.
			proxyResp.Headers = upResp.Header.Clone()
		}
	}

	// Run plugin OnResponse chain (with panic recovery).
	var finalRespBody = respBody
	var pluginRespErr error
	if p.plugins != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logging.Errorf(ctx, "HTTP proxy panic in plugin OnResponse (recovered): %v", rec)
				}
			}()
			// Give plugins the body they expect
			proxyResp.Body = io.NopCloser(bytes.NewReader(finalRespBody))
			modResp, err := p.plugins.RunResponse(ctx, proxyReq, proxyResp)
			if err != nil {
				logging.Errorf(ctx, "plugin OnResponse error: %v", err)
				pluginRespErr = err
			} else if modResp != nil {
				proxyResp = modResp
				// Extract body from modified response if provided
				if modResp.Body != nil {
					limitedBody := io.LimitReader(modResp.Body, maxBodyBytes)
					var readErr error
					finalRespBody, readErr = io.ReadAll(limitedBody)
					if readErr != nil {
						logging.Errorf(ctx, "plugin modified response body read error: %v", readErr)
						// Keep the original respBody on error
					}
				}
			}
		}()
	}
	if pluginRespErr != nil {
		http.Error(w, fmt.Sprintf("plugin OnResponse error: %v", pluginRespErr), http.StatusBadGateway)
		return
	}

	// Store transaction.
	if p.engine != nil {
		tx.Response = proxyResp
		tx.ResponseBody = finalRespBody
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "store transaction: %v", err)
		}
	}

	// Observe metrics
	durationSec := time.Since(start).Seconds()
	metrics.ObserveRequest(proxyReq.Method, proxyResp.StatusCode, durationSec)

	// Slowlog
	if p.cfg != nil && p.cfg.SlowlogThresholdMs > 0 {
		if time.Since(start).Milliseconds() > int64(p.cfg.SlowlogThresholdMs) {
			logging.Warnf(ctx, "slow request: method=%s url=%s status=%d duration_ms=%d request_id=%s",
				proxyReq.Method, proxyReq.URL.String(), proxyResp.StatusCode, time.Since(start).Milliseconds(), reqID)
		}
	}

	writeProxyResponse(w, proxyResp, finalRespBody)
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
		if _, err := w.Write(body); err != nil {
			rid := resp.Headers.Get(logging.RequestIDHeader)
			ctx := context.Background()
			if rid != "" {
				ctx = logging.WithRequestID(ctx, rid)
			}
			logging.Errorf(ctx, "proxy: write response to client: %v", err)
		}
	}
}

// SetPlugins wires the plugin chain into the HTTP proxy.
func (p *HTTPProxy) SetPlugins(chain PluginChain) {
	p.plugins = chain
}

// Close gracefully shuts down the HTTP proxy and closes idle connections.
func (p *HTTPProxy) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}
