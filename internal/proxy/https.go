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
	limiter       *proxyRateLimiter
}

// NewTLSProxy creates a MITM TLS proxy using the provided CA.
func NewTLSProxy(ca *CertAuthority, engine TrafficEngine, opts TransportOptions, cfg *config.Config) *TLSProxy {
	return &TLSProxy{
		ca:            ca,
		engine:        engine,
		transportOpts: opts,
		transport:     newTransport(nil, opts),
		cfg:           cfg,
		limiter:       newProxyRateLimiter(cfg),
	}
}

// SetPlugins wires the plugin chain into the TLS proxy. chain may implement
// RequestPlugin, ResponsePlugin, or both (PluginChain). Passing nil disables
// plugin execution.
func (p *TLSProxy) SetPlugins(chain any) {
	p.plugins = chain
}

func (p *TLSProxy) SetRateLimiter(limiter *proxyRateLimiter) {
	p.limiter = limiter
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
		recoverProxyPanic(ctx, "TLS proxy panic", func() {
			writeHTTPError(ctx, conn, http.StatusBadGateway, "proxy error")
		})
	}()

	reqID := uuid.NewString()
	start := time.Now()
	origHeaders := r.Header.Clone()
	clientIP := normalizeClientIP(conn.RemoteAddr().String())
	if !p.limiter.allow(clientIP) {
		writeHTTPError(ctx, conn, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	if err := validateInboundRequest(p.cfg, r); err != nil {
		writeHTTPError(ctx, conn, http.StatusRequestHeaderFieldsTooLarge, err.Error())
		return
	}

	// Instrument active connections
	metrics.IncActive()
	defer metrics.DecActive()
	p.processRequestPipeline(ctx, r, reqID, protocol, start, origHeaders, "tls proxy", tlsPipelineIO{
		writeError: func(code int, msg string) {
			writeHTTPError(ctx, conn, code, msg)
		},
		writeResponse: func(resp *plugins.ProxyResponse, body []byte) {
			writeProxyResponseToConnWithBody(ctx, conn, resp, body)
		},
		handleWebSocket: func(tx *Transaction) {
			p.handleWebSocket(ctx, conn, br, r, tx, start)
		},
	})
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
		recoverProxyPanic(ctx, "TLS proxy HTTP/2 panic", func() {
			http.Error(w, "proxy error", http.StatusBadGateway)
		})
	}()

	reqID := uuid.NewString()
	start := time.Now()
	clientIP := normalizeClientIP(r.RemoteAddr)
	if !p.limiter.allow(clientIP) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if r.URL.Host == "" {
		r.URL.Host = host
	}
	if r.URL.Scheme == "" {
		r.URL.Scheme = "https"
	}
	if err := validateInboundRequest(p.cfg, r); err != nil {
		http.Error(w, err.Error(), http.StatusRequestHeaderFieldsTooLarge)
		return
	}

	origHeaders := r.Header.Clone()
	metrics.IncActive()
	defer metrics.DecActive()
	p.processRequestPipeline(ctx, r, reqID, "HTTP/2.0", start, origHeaders, "tls proxy h2", tlsPipelineIO{
		writeError: func(code int, msg string) {
			http.Error(w, msg, code)
		},
		writeResponse: func(resp *plugins.ProxyResponse, body []byte) {
			writeProxyResponse(w, resp, body)
		},
	})
}

type tlsPipelineIO struct {
	writeError      func(code int, msg string)
	writeResponse   func(resp *plugins.ProxyResponse, body []byte)
	handleWebSocket func(tx *Transaction)
}

func (p *TLSProxy) processRequestPipeline(ctx context.Context, r *http.Request, reqID, protocol string, start time.Time, origHeaders http.Header, logPrefix string, ioHooks tlsPipelineIO) {
	maxReqBodyBytes := maxBodyBytes(p.cfg)
	bodyBytes, err := p.readTLSRequestBody(r, maxReqBodyBytes)
	if err != nil {
		ioHooks.writeError(http.StatusRequestEntityTooLarge, fmt.Sprintf("request body too large: %v", err))
		return
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

	proxyReq, err = runPluginRequest(ctx, p.plugins, proxyReq, logPrefix)
	if err != nil {
		ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("plugin error: %v", err))
		return
	}

	if proxyReq.MockedResponse != nil {
		mockBody, bodyErr := responseBodyForWrite(proxyReq.MockedResponse, nil)
		if bodyErr != nil {
			ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("read mocked response body: %v", bodyErr))
			return
		}
		ioHooks.writeResponse(proxyReq.MockedResponse, mockBody)
		return
	}

	tx := &Transaction{
		ID:                     reqID,
		Request:                proxyReq,
		RequestBody:            bodyBytes,
		OriginalRequestHeaders: origHeaders,
	}
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseRequest(tx)
		if bpErr != nil {
			logging.Errorf(ctx, "%s: pause request: %v", logPrefix, bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			ioHooks.writeError(http.StatusBadGateway, "request dropped by breakpoint")
			return
		case ResumeRespond:
			if tx.Response == nil {
				ioHooks.writeError(http.StatusBadGateway, "no synthetic response")
				return
			}
			respBody, bodyErr := responseBodyForWrite(tx.Response, tx.ResponseBody)
			if bodyErr != nil {
				ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("read synthetic response body: %v", bodyErr))
				return
			}
			ioHooks.writeResponse(tx.Response, respBody)
			return
		}
	}

	if ioHooks.handleWebSocket != nil && isWebSocketRequest(r) {
		ioHooks.handleWebSocket(tx)
		return
	}

	upReq, upErr := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if upErr != nil {
		ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("build upstream request: %v", upErr))
		return
	}
	upReq.Header = proxyReq.Headers.Clone()
	upResp, upErr := p.transport.RoundTrip(upReq)
	if upErr != nil {
		ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("upstream error: %v", upErr))
		return
	}
	defer func() { _ = upResp.Body.Close() }()

	respBody, readErr := httputil.ReadLimitedBody(upResp.Body, maxReqBodyBytes)
	if readErr != nil {
		ioHooks.writeError(http.StatusRequestEntityTooLarge, fmt.Sprintf("response body too large: %v", readErr))
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
	proxyResp, err = runPluginResponse(ctx, p.plugins, proxyReq, proxyResp, logPrefix)
	if err != nil {
		ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("plugin OnResponse error: %v", err))
		return
	}

	finalRespBody, readErr := responseBodyForWrite(proxyResp, respBody)
	if readErr != nil {
		ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("read response body: %v", readErr))
		return
	}

	tx.Response = proxyResp
	tx.ResponseBody = finalRespBody
	if p.engine != nil {
		modified, action, bpErr := p.engine.PauseResponse(tx, proxyResp.StatusCode, finalRespBody)
		if bpErr != nil {
			logging.Errorf(ctx, "%s: pause response: %v", logPrefix, bpErr)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			ioHooks.writeError(http.StatusBadGateway, "response dropped by breakpoint")
			return
		case ResumeRespond:
			if tx.Response == nil {
				ioHooks.writeError(http.StatusBadGateway, "no synthetic response")
				return
			}
			respBody, bodyErr := responseBodyForWrite(tx.Response, tx.ResponseBody)
			if bodyErr != nil {
				ioHooks.writeError(http.StatusBadGateway, fmt.Sprintf("read synthetic response body: %v", bodyErr))
				return
			}
			ioHooks.writeResponse(tx.Response, respBody)
			return
		}
	}

	storeAndObserve(ctx, p.cfg, p.engine, tx, start, logPrefix+": store transaction")
	ioHooks.writeResponse(tx.Response, tx.ResponseBody)
}

func (p *TLSProxy) readTLSRequestBody(r *http.Request, maxBodyBytes int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	bodyBytes, err := httputil.ReadLimitedBody(r.Body, maxBodyBytes)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return bodyBytes, nil
}

func responseBodyForWrite(resp *plugins.ProxyResponse, fallback []byte) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		if fallback == nil {
			return []byte{}, nil
		}
		return fallback, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
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
				writeHTTPError(ctx, conn, http.StatusBadGateway, fmt.Sprintf("websocket upstream error: %v: %s", err, msg))
				return
			}
		}
		writeHTTPError(ctx, conn, http.StatusBadGateway, fmt.Sprintf("websocket upstream error: %v", err))
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	if protocol := upstreamConn.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	responseWriter := newHijackableResponseWriter(ctx, conn, br)
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
func writeHTTPError(ctx context.Context, conn net.Conn, code int, msg string) {
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
		logging.Errorf(ctx, "tls proxy: write error response to client: %v", err)
	}
}

func writeProxyResponseToConnWithBody(ctx context.Context, conn net.Conn, resp *plugins.ProxyResponse, body []byte) {
	if body == nil {
		body = []byte{}
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
		logging.Errorf(ctx, "tls proxy: write response to client: %v", err)
	}
}

// Close gracefully shuts down the TLS proxy and closes idle connections.
func (p *TLSProxy) Close() {
	if p.transport != nil {
		p.transport.CloseIdleConnections()
	}
}
