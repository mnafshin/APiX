package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/pkg/plugins"
)

// TLSProxy performs MITM TLS interception.
type TLSProxy struct {
	ca      *CertAuthority
	engine  TrafficEngine
	plugins PluginChain
}

// NewTLSProxy creates a MITM TLS proxy using the provided CA.
func NewTLSProxy(ca *CertAuthority, engine TrafficEngine) *TLSProxy {
	return &TLSProxy{
		ca:     ca,
		engine: engine,
	}
}

// SetPlugins wires the plugin chain into the TLS proxy.
func (p *TLSProxy) SetPlugins(chain PluginChain) {
	p.plugins = chain
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
		log.Printf("tls proxy: cert for %s: %v", hostname, err)
		return
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	tlsConn := tls.Server(conn, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("tls proxy: handshake for %s: %v", hostname, err)
		return
	}
	defer tlsConn.Close()

	br := bufio.NewReader(tlsConn)

	// Handle multiple pipelined requests on the same connection.
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			if err != io.EOF {
				log.Printf("tls proxy: read request from %s: %v", hostname, err)
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
	reqID := uuid.NewString()
	start := time.Now()

	proxyReq := &plugins.ProxyRequest{
		ID:      reqID,
		Method:  r.Method,
		URL:     r.URL,
		Headers: r.Header.Clone(),
		Body:    r.Body,
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
	tx := &Transaction{ID: reqID, Request: proxyReq}
	if p.engine != nil {
		modified, action, err := p.engine.PauseRequest(tx)
		if err != nil {
			log.Printf("tls proxy: pause request: %v", err)
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

	// Forward to upstream.
	upConn, err := p.dialUpstream(ctx, host)
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("upstream connect: %v", err))
		return
	}
	defer upConn.Close()

	// Send request upstream.
	if err := r.Write(upConn); err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("write upstream: %v", err))
		return
	}

	// Read upstream response.
	upResp, err := http.ReadResponse(bufio.NewReader(upConn), r)
	if err != nil {
		writeHTTPError(conn, http.StatusBadGateway, fmt.Sprintf("read upstream response: %v", err))
		return
	}
	defer upResp.Body.Close()

	respBody, _ := io.ReadAll(upResp.Body)
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
			log.Printf("tls proxy: plugin OnResponse: %v", err)
		} else if modResp != nil {
			proxyResp = modResp
		}
	}

	// Store transaction.
	if p.engine != nil {
		tx.Response = proxyResp
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			log.Printf("tls proxy: store transaction: %v", err)
		}
	}

	writeProxyResponseToConn(conn, proxyResp)
}

// dialUpstream opens a TLS connection to the target host.
func (p *TLSProxy) dialUpstream(ctx context.Context, host string) (*tls.Conn, error) {
	// Ensure host has a port.
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = host + ":443"
	}
	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}
	hostname, _, _ := net.SplitHostPort(host)
	tlsConn := tls.Client(rawConn, &tls.Config{ServerName: hostname})
	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("tls handshake with %s: %w", host, err)
	}
	return tlsConn, nil
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
	_ = resp.Write(conn)
}

// writeProxyResponseToConn serialises a ProxyResponse to a net.Conn.
func writeProxyResponseToConn(conn net.Conn, resp *plugins.ProxyResponse) {
	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(resp.Body)
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
	_ = httpResp.Write(conn)
}
