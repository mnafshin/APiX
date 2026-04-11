package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	logging "github.com/mnafshin/apix/internal/logging"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/internal/config"
	"github.com/mnafshin/apix/pkg/plugins"
)

// TLSProxy performs MITM TLS interception.
type TLSProxy struct {
	ca            *CertAuthority
	engine        TrafficEngine
	plugins       PluginChain
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

// SetPlugins wires the plugin chain into the TLS proxy.
func (p *TLSProxy) SetPlugins(chain PluginChain) {
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
	defer conn.Close()

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
	}
	tlsConn := tls.Server(conn, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		logging.Errorf(ctx, "tls proxy: handshake for %s: %v", hostname, err)
		return
	}
	defer tlsConn.Close()

	br := bufio.NewReader(tlsConn)

	// Handle multiple pipelined requests on the same connection.
	for {
		req, err := http.ReadRequest(br)
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

		p.handleRequest(ctx, tlsConn, req, host)
	}
}

func (p *TLSProxy) handleRequest(ctx context.Context, conn net.Conn, r *http.Request, host string) {
	defer func() {
		if rec := recover(); rec != nil {
			logging.Errorf(ctx, "TLS proxy panic (recovered): %v", rec)
			writeHTTPError(conn, http.StatusBadGateway, "proxy error")
		}
	}()

	reqID := uuid.NewString()
	start := time.Now()

	// Buffer the entire request body so it can be stored and forwarded.
	// Limit body size to prevent OOM denial of service.
	maxBodyBytes := int64(p.cfg.MaxBodySizeMB) * 1024 * 1024
	var bodyBytes []byte
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			writeHTTPError(conn, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body too large: %v", err))
			return
		}
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	proxyReq := &plugins.ProxyRequest{
		ID:      reqID,
		Method:  r.Method,
		URL:     r.URL,
		Headers: r.Header.Clone(),
		Body:    io.NopCloser(bytes.NewReader(bodyBytes)),
		Raw:     r,
	}

	// Run plugin OnRequest chain.
	if p.plugins != nil {
		modified, err := p.plugins.RunRequest(ctx, proxyReq)
		if err != nil {
			writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("plugin error: %v", err))
			return
		}
		proxyReq = modified
	}

	// Mocked response short-circuit.
	if proxyReq.MockedResponse != nil {
		writeProxyResponseToConn(conn, proxyReq.MockedResponse)
		return
	}

	// Breakpoint check.
	tx := &Transaction{ID: reqID, Request: proxyReq, RequestBody: bodyBytes}
	if p.engine != nil {
		modified, action, err := p.engine.PauseRequest(tx)
		if err != nil {
			logging.Errorf(ctx, "tls proxy: pause request: %v", err)
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

	// Build and send upstream request using the shared pooled transport.
	upReq, err := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("build upstream request: %v", err))
		return
	}
	upReq.Header = proxyReq.Headers.Clone()

	upResp, err := p.transport.RoundTrip(upReq)
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		return
	}
	defer upResp.Body.Close()

	// Apply the same body size limit to response bodies
	upResp.Body = http.MaxBytesReader(nil, upResp.Body, maxBodyBytes)
	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		writeHTTPError(conn, http.StatusRequestEntityTooLarge, fmt.Sprintf("response body too large: %v", err))
		return
	}
	proxyResp := &plugins.ProxyResponse{
		StatusCode: upResp.StatusCode,
		Status:     upResp.Status,
		Headers:    upResp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Raw:        upResp,
	}

	// Run plugin OnResponse chain.
	if p.plugins != nil {
		modResp, err := p.plugins.RunResponse(ctx, proxyReq, proxyResp)
		if err != nil {
			logging.Errorf(ctx, "tls proxy: plugin OnResponse: %v", err)
		} else if modResp != nil {
			proxyResp = modResp
		}
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
	if p.engine != nil {
		tx.Response = proxyResp
		tx.ResponseBody = finalRespBody
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			logging.Errorf(ctx, "tls proxy: store transaction: %v", err)
		}
	}

	writeProxyResponseToConn(conn, proxyResp)
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
