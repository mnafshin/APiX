package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mnafshin/apix/pkg/plugins"
)

// HTTPProxy is a forward proxy that intercepts plain HTTP traffic and tunnels
// HTTPS CONNECT requests to the MITM TLS proxy.
type HTTPProxy struct {
	addr     string
	tlsProxy *TLSProxy
	plugins  PluginChain
	engine   TrafficEngine
}

// NewHTTPProxy creates a new HTTP proxy listening on addr.
func NewHTTPProxy(addr string, tlsProxy *TLSProxy, engine TrafficEngine) *HTTPProxy {
	return &HTTPProxy{
		addr:     addr,
		tlsProxy: tlsProxy,
		engine:   engine,
	}
}

// Start begins accepting connections. Blocks until ctx is cancelled.
func (p *HTTPProxy) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    p.addr,
		Handler: p,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("http proxy shutdown: %v", err)
		}
	}()
	log.Printf("HTTP proxy listening on %s", p.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http proxy: %w", err)
	}
	return nil
}

// ServeHTTP handles both plain HTTP and CONNECT (tunnel) requests.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleConnect upgrades the connection for HTTPS tunnelling.
func (p *HTTPProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		log.Printf("hijack: %v", err)
		return
	}

	// Flush any buffered data from the hijacked reader.
	if brw.Reader.Buffered() > 0 {
		conn.Close()
		log.Printf("handleConnect: unexpected buffered data after hijack")
		return
	}

	// Respond 200 Connection established.
	_, err = fmt.Fprint(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
	if err != nil {
		conn.Close()
		return
	}

	p.tlsProxy.HandleConn(r.Context(), conn, r.Host)
}

// handleHTTP proxies a plain HTTP request, runs plugin chain, stores traffic.
func (p *HTTPProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	reqID := uuid.NewString()
	start := time.Now()

	// Buffer the entire request body so it can be stored and forwarded.
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read request body: %v", err), http.StatusBadGateway)
			return
		}
		r.Body.Close()
	}

	// Convert net/http request to ProxyRequest.
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
			http.Error(w, fmt.Sprintf("plugin error: %v", err), http.StatusBadGateway)
			return
		}
		proxyReq = modified
	}

	// If a plugin set a mocked response, short-circuit.
	if proxyReq.MockedResponse != nil {
		writeProxyResponse(w, proxyReq.MockedResponse)
		return
	}

	// Check breakpoints.
	tx := &Transaction{
		ID:          reqID,
		Request:     proxyReq,
		RequestBody: bodyBytes,
	}
	if p.engine != nil {
		bpID := "" // The engine handles evaluation internally via PauseRequest.
		_ = bpID
		modified, action, err := p.engine.PauseRequest(tx)
		if err != nil {
			log.Printf("pause request: %v", err)
		}
		tx = modified
		switch action {
		case ResumeDrop:
			http.Error(w, "request dropped by breakpoint", http.StatusBadGateway)
			return
		case ResumeRespond:
			if tx.Response != nil {
				writeProxyResponse(w, tx.Response)
			} else {
				http.Error(w, "no synthetic response provided", http.StatusBadGateway)
			}
			return
		}
	}

	// Build upstream request.
	upReq, err := http.NewRequestWithContext(ctx, proxyReq.Method, proxyReq.URL.String(), proxyReq.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("build upstream request: %v", err), http.StatusBadGateway)
		return
	}
	upReq.Header = proxyReq.Headers

	transport := &http.Transport{}
	upResp, err := transport.RoundTrip(upReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		return
	}
	defer upResp.Body.Close()

	// Convert to ProxyResponse.
	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read upstream response body: %v", err), http.StatusBadGateway)
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
			log.Printf("plugin OnResponse: %v", err)
		} else if modResp != nil {
			proxyResp = modResp
		}
	}

	// Store transaction.
	if p.engine != nil {
		tx.Response = proxyResp
		tx.DurationMs = time.Since(start).Milliseconds()
		if err := p.engine.StoreTransaction(tx); err != nil {
			log.Printf("store transaction: %v", err)
		}
	}

	writeProxyResponse(w, proxyResp)
}

// writeProxyResponse writes a ProxyResponse back to the http.ResponseWriter.
func writeProxyResponse(w http.ResponseWriter, resp *plugins.ProxyResponse) {
	for k, vv := range resp.Headers {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if resp.Body != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("proxy: read response body for write: %v", err)
			return
		}
		if _, err := w.Write(body); err != nil {
			log.Printf("proxy: write response to client: %v", err)
		}
	}
}

// SetPlugins wires the plugin chain into the HTTP proxy.
func (p *HTTPProxy) SetPlugins(chain PluginChain) {
	p.plugins = chain
}
