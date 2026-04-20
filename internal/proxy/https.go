package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	metrics "github.com/mnafshin/apix/internal/metrics"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mnafshin/apix/internal/config"
	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/pkg/plugins"
	"golang.org/x/net/http2"
)

// TLSProxy performs MITM TLS interception.
type TLSProxy struct {
	ca            *CertAuthority
	engine        TrafficEngine
	plugins       any
	transport     *http.Transport
	transportOpts TransportOptions // retained so SetUpstreamTLSConfig can rebuild
	cfg           *config.Config
}

// NewTLSProxy creates a MITM TLS proxy using the provided CA.
func NewTLSProxy(ca *CertAuthority, engine TrafficEngine, opts TransportOptions, cfg *config.Config) *TLSProxy {
	return &TLSProxy{
		ca:            ca,
		engine:        engine,
		transportOpts: opts,
		transport:     newTransport(nil, opts),
		cfg:           cfg,
	}
}

// SetPlugins wires the plugin chain into the TLS proxy. chain may implement
// RequestPlugin, ResponsePlugin, or both (PluginChain). Passing nil disables
// plugin execution.
func (p *TLSProxy) SetPlugins(chain any) {
	p.plugins = chain
}

// SetUpstreamTLSConfig sets a custom TLS configuration used when dialling the
// upstream server. Primarily useful in tests to trust self-signed test certs.
// Calling this replaces the shared transport so the new TLS config takes effect.
func (p *TLSProxy) SetUpstreamTLSConfig(cfg *tls.Config) {
	p.transport = newTransport(cfg, p.transportOpts)
}

// CACertPEM returns the PEM-encoded CA certificate so callers can build a
// trusted cert pool for clients connecting through the MITM proxy.
func (p *TLSProxy) CACertPEM() ([]byte, error) {
	return p.ca.CACertPEM()
}

// HandleConn performs MITM interception on a raw TCP connection destined for host.
func (p *TLSProxy) HandleConn(ctx context.Context, conn net.Conn, host string) {
	p.handleBufferedConn(ctx, conn, host, bufio.NewReader(conn))
}

func (p *TLSProxy) handleBufferedConn(ctx context.Context, conn net.Conn, host string, br *bufio.Reader) {
	defer func() { _ = conn.Close() }()

	// Strip port from host for cert generation (SNI requires just the hostname).
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	cert, err := p.ca.CertForHost(hostname)
	if err != nil {
		logging.Errorf(ctx, "tls proxy: cert for %s: %v", hostname, err)
		return
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}
	tlsConn := tls.Server(&bufferedConn{Conn: conn, reader: br}, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		logging.Errorf(ctx, "tls proxy: handshake for %s: %v", hostname, err)
		return
	}
	defer func() { _ = tlsConn.Close() }()

	// Detect ALPN-negotiated protocol. "h2" means HTTP/2 over TLS.
	tlsProto := "HTTP/1.1"
	if cs := tlsConn.ConnectionState(); cs.NegotiatedProtocol == "h2" {
		tlsProto = "HTTP/2.0"
	}

	tlsBr := bufio.NewReader(tlsConn)
	if tlsProto == "HTTP/2.0" {
		p.handleHTTP2Conn(ctx, tlsConn, tlsBr, host)
		return
	}

	// Handle multiple pipelined requests on the same connection.
	for {
		req, err := http.ReadRequest(tlsBr)
		if err != nil {
			if err != io.EOF {
				logging.Errorf(ctx, "tls proxy: read request from %s: %v", hostname, err)
			}
			return
		}

		// Ensure the request URL is absolute.
		if req.URL.Host == "" {
			req.URL.Host = host
		}
		if req.URL.Scheme == "" {
			req.URL.Scheme = "https"
		}

		p.handleRequest(ctx, tlsConn, tlsBr, req, host, tlsProto)
		if isWebSocketRequest(req) {
			return
		}
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (p *TLSProxy) handleRequest(ctx context.Context, conn net.Conn, br *bufio.Reader, r *http.Request, host string, protocol string) {
	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "TLS proxy panic (recovered): %v", rec)
			writeHTTPError(conn, http.StatusBadGateway, "proxy error")
		}
	}()

	reqID := uuid.NewString()
	start := time.Now()
	origHeaders := r.Header.Clone()

	// Instrument active connections
	metrics.IncActive()
	defer metrics.DecActive()

	// Buffer the entire request body so it can be stored and forwarded.
	// Limit body size to prevent OOM denial of service.
	maxBodyBytes := int64(p.cfg.MaxBodySizeMB) * 1024 * 1024
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = httputil.ReadLimitedBody(r.Body, maxBodyBytes)
		if err != nil {
			writeHTTPError(conn, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body too large: %v", err))
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	annotateGRPCFrames(origHeaders, bodyBytes)

	proxyReq := &plugins.ProxyRequest{
		ID:       reqID,
		Method:   r.Method,
		URL:      r.URL,
		Headers:  r.Header.Clone(),
		Body:     io.NopCloser(bytes.NewReader(bodyBytes)),
		Protocol: protocol,
		Raw:      r,
	}

	// Run plugin OnRequest chain with panic recovery.
	var err error
	proxyReq, err = runPluginRequest(ctx, p.plugins, proxyReq, "tls proxy")
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("plugin error: %v", err))
		return
	}

	// Mocked response short-circuit.
	if proxyReq.MockedResponse != nil {
		writeProxyResponseToConn(conn, proxyReq.MockedResponse)
		return
	}

	// Breakpoint check.
	tx := &Transaction{ID: reqID, Request: proxyReq, RequestBody: bodyBytes, OriginalRequestHeaders: origHeaders}
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseRequest(tx)
		if bpErr != nil {
			logging.Errorf(ctx, "tls proxy: pause request: %v", bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			writeHTTPError(conn, http.StatusBadGateway, "request dropped by breakpoint")
			return
		case ResumeRespond:
			if tx.Response != nil {
				writeProxyResponseToConn(conn, tx.Response)
			} else {
				writeHTTPError(conn, http.StatusBadGateway, "no synthetic response")
			}
			return
		}
	}

	if isWebSocketRequest(r) {
		p.handleWebSocket(ctx, conn, br, r, tx, start)
		return
	}

	// Build and send upstream request using the shared pooled transport.
	upReq, upErr := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if upErr != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("build upstream request: %v", upErr))
		return
	}
	upReq.Header = proxyReq.Headers.Clone()

	upResp, upErr := p.transport.RoundTrip(upReq)
	if upErr != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", upErr))
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	// Apply the same body size limit to response bodies
	respBody, readErr := httputil.ReadLimitedBody(upResp.Body, maxBodyBytes)
	if readErr != nil {
		writeHTTPError(conn, http.StatusRequestEntityTooLarge, fmt.Sprintf("response body too large: %v", readErr))
		return
	}
	mergeTrailersIntoHeaders(upResp.Header, upResp.Trailer)
	setGRPCStatusFromTrailers(upResp.Header)
	annotateGRPCFrames(upResp.Header, respBody)
	proxyResp := &plugins.ProxyResponse{
		StatusCode: upResp.StatusCode,
		Status:     upResp.Status,
		Headers:    upResp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Raw:        upResp,
	}

	// Run plugin OnResponse chain with panic recovery.
	proxyResp, err = runPluginResponse(ctx, p.plugins, proxyReq, proxyResp, "tls proxy")
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("plugin OnResponse error: %v", err))
		return
	}

	// Buffer the final response body (plugins may have modified it) so we can
	// both persist it and still write it to the client.
	var finalRespBody []byte
	if proxyResp.Body != nil {
		var readErr error
		finalRespBody, readErr = io.ReadAll(proxyResp.Body)
		if readErr != nil {
			writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("read response body: %v", readErr))
			return
		}
		proxyResp.Body = io.NopCloser(bytes.NewReader(finalRespBody))
	}

	// Store transaction.
	tx.Response = proxyResp
	tx.ResponseBody = finalRespBody
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseResponse(tx, proxyResp.StatusCode, finalRespBody)
		if bpErr != nil {
			logging.Errorf(ctx, "tls proxy: pause response: %v", bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			writeHTTPError(conn, http.StatusBadGateway, "response dropped by breakpoint")
			return
		case ResumeRespond:
			if tx.Response != nil {
				writeProxyResponseToConn(conn, tx.Response)
			} else {
				writeHTTPError(conn, http.StatusBadGateway, "no synthetic response")
			}
			return
		}
	}

	// Store transaction.
	if p.engine != nil {
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "tls proxy: store transaction: %v", err)
		}
	}

	// Observe metrics + slowlog.
	dur := time.Since(start)
	observeRequest(ctx, p.cfg, proxyReq.Method, proxyReq.URL.Host, tx.Response.StatusCode, dur)
	_ = reqID

	writeProxyResponseToConn(conn, tx.Response)
}

func (p *TLSProxy) handleHTTP2Conn(ctx context.Context, conn net.Conn, br *bufio.Reader, host string) {
	h2 := &http2.Server{}
	h2.ServeConn(&bufferedConn{Conn: conn, reader: br}, &http2.ServeConnOpts{
		Context: context.WithoutCancel(ctx),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p.handleHTTP2Request(ctx, w, r, host)
		}),
	})
}

func (p *TLSProxy) handleHTTP2Request(ctx context.Context, w http.ResponseWriter, r *http.Request, host string) {
	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "TLS proxy HTTP/2 panic (recovered): %v", rec)
			http.Error(w, "proxy error", http.StatusBadGateway)
		}
	}()

	reqID := uuid.NewString()
	start := time.Now()
	if r.URL.Host == "" {
		r.URL.Host = host
	}
	if r.URL.Scheme == "" {
		r.URL.Scheme = "https"
	}

	origHeaders := r.Header.Clone()
	metrics.IncActive()
	defer metrics.DecActive()

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
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	annotateGRPCFrames(origHeaders, bodyBytes)

	proxyReq := &plugins.ProxyRequest{
		ID:       reqID,
		Method:   r.Method,
		URL:      r.URL,
		Headers:  r.Header.Clone(),
		Body:     io.NopCloser(bytes.NewReader(bodyBytes)),
		Protocol: "HTTP/2.0",
		Raw:      r,
	}

	var err error
	proxyReq, err = runPluginRequest(ctx, p.plugins, proxyReq, "tls proxy h2")
	if err != nil {
		http.Error(w, fmt.Sprintf("plugin error: %v", err), http.StatusBadGateway)
		return
	}

	if proxyReq.MockedResponse != nil {
		mockBody := []byte{}
		if proxyReq.MockedResponse.Body != nil {
			mockBody, err = io.ReadAll(proxyReq.MockedResponse.Body)
			if err != nil {
				http.Error(w, fmt.Sprintf("read mocked response body: %v", err), http.StatusBadGateway)
				return
			}
		}
		writeProxyResponse(w, proxyReq.MockedResponse, mockBody)
		return
	}

	tx := &Transaction{ID: reqID, Request: proxyReq, RequestBody: bodyBytes, OriginalRequestHeaders: origHeaders}
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseRequest(tx)
		if bpErr != nil {
			logging.Errorf(ctx, "tls proxy h2: pause request: %v", bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			http.Error(w, "request dropped by breakpoint", http.StatusBadGateway)
			return
		case ResumeRespond:
			if tx.Response != nil {
				respBody := tx.ResponseBody
				if tx.Response.Body != nil {
					respBody, err = io.ReadAll(tx.Response.Body)
					if err != nil {
						http.Error(w, fmt.Sprintf("read synthetic response body: %v", err), http.StatusBadGateway)
						return
					}
				}
				writeProxyResponse(w, tx.Response, respBody)
			} else {
				http.Error(w, "no synthetic response", http.StatusBadGateway)
			}
			return
		}
	}

	upReq, upErr := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if upErr != nil {
		http.Error(w, fmt.Sprintf("build upstream request: %v", upErr), http.StatusBadGateway)
		return
	}
	upReq.Header = proxyReq.Headers.Clone()
	upResp, upErr := p.transport.RoundTrip(upReq)
	if upErr != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", upErr), http.StatusBadGateway)
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	respBody, readErr := httputil.ReadLimitedBody(upResp.Body, maxBodyBytes)
	if readErr != nil {
		http.Error(w, fmt.Sprintf("response body too large: %v", readErr), http.StatusRequestEntityTooLarge)
		return
	}
	mergeTrailersIntoHeaders(upResp.Header, upResp.Trailer)
	setGRPCStatusFromTrailers(upResp.Header)
	annotateGRPCFrames(upResp.Header, respBody)

	proxyResp := &plugins.ProxyResponse{
		StatusCode: upResp.StatusCode,
		Status:     upResp.Status,
		Headers:    upResp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Raw:        upResp,
	}

	proxyResp, err = runPluginResponse(ctx, p.plugins, proxyReq, proxyResp, "tls proxy h2")
	if err != nil {
		http.Error(w, fmt.Sprintf("plugin OnResponse error: %v", err), http.StatusBadGateway)
		return
	}
	finalRespBody := respBody
	if proxyResp.Body != nil {
		finalRespBody, readErr = io.ReadAll(proxyResp.Body)
		if readErr != nil {
			http.Error(w, fmt.Sprintf("read response body: %v", readErr), http.StatusBadGateway)
			return
		}
	}

	tx.Response = proxyResp
	tx.ResponseBody = finalRespBody
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseResponse(tx, proxyResp.StatusCode, finalRespBody)
		if bpErr != nil {
			logging.Errorf(ctx, "tls proxy h2: pause response: %v", bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			http.Error(w, "response dropped by breakpoint", http.StatusBadGateway)
			return
		case ResumeRespond:
			if tx.Response != nil {
				writeProxyResponse(w, tx.Response, tx.ResponseBody)
			} else {
				http.Error(w, "no synthetic response", http.StatusBadGateway)
			}
			return
		}
	}

	if p.engine != nil {
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "tls proxy h2: store transaction: %v", err)
		}
	}
	observeRequest(ctx, p.cfg, proxyReq.Method, proxyReq.URL.Host, tx.Response.StatusCode, time.Since(start))
	writeProxyResponse(w, tx.Response, tx.ResponseBody)
}

func (p *TLSProxy) handleWebSocket(ctx context.Context, conn net.Conn, br *bufio.Reader, clientReq *http.Request, tx *Transaction, start time.Time) {
	upstreamHeaders := copyHeadersExcluding(tx.Request.Headers, "Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Host")
	dialer := newWebSocketDialer(p.transport, tx.Request)
	upstreamConn, resp, err := dialer.DialContext(ctx, webSocketTargetURL(tx.Request.URL), upstreamHeaders)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
			msg, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
			if readErr == nil && len(msg) > 0 {
				writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("websocket upstream error: %v: %s", err, msg))
				return
			}
		}
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("websocket upstream error: %v", err))
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	if protocol := upstreamConn.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	responseWriter := newHijackableResponseWriter(conn, br)
	clientConn, err := upgrader.Upgrade(responseWriter, clientReq, copyHeadersExcluding(resp.Header, "Connection", "Upgrade", "Sec-WebSocket-Accept", "Sec-WebSocket-Extensions"))
	if err != nil {
		_ = upstreamConn.Close()
		logging.Errorf(ctx, "tls websocket client upgrade: %v", err)
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
			logging.Errorf(ctx, "tls websocket store transaction: %v", err)
		}
	}

	relayWebSocket(ctx, p.engine, tx.ID, clientConn, upstreamConn)
}

// writeHTTPError writes a minimal HTTP error response to the connection.
func writeHTTPError(conn net.Conn, code int, msg string) {
	resp := &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(msg)),
	}
	resp.ContentLength = int64(len(msg))
	if err := resp.Write(conn); err != nil {
		logging.Errorf(context.Background(), "tls proxy: write error response to client: %v", err)
	}
}

// writeProxyResponseToConn serialises a ProxyResponse to a net.Conn.
func writeProxyResponseToConn(conn net.Conn, resp *plugins.ProxyResponse) {
	var body []byte
	if resp.Body != nil {
		var err error
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			logging.Errorf(context.Background(), "tls proxy: read response body for write: %v", err)
			return
		}
	}
	httpResp := &http.Response{
		StatusCode:    resp.StatusCode,
		Status:        resp.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        resp.Headers,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	if err := httpResp.Write(conn); err != nil {
		logging.Errorf(context.Background(), "tls proxy: write response to client: %v", err)
	}
}

// Close gracefully shuts down the TLS proxy and closes idle connections.
func (p *TLSProxy) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}
