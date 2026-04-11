package proxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// TransportOptions configures the shared upstream HTTP connection pool.
// Zero values fall back to built-in defaults.
type TransportOptions struct {
	MaxIdleConnsPerHost   int           // default: 10
	IdleConnTimeout       time.Duration // default: 90s
	DialTimeout           time.Duration // default: 10s
	TLSHandshakeTimeout   time.Duration // default: 10s
	ResponseHeaderTimeout time.Duration // default: 30s
	ExpectContinueTimeout time.Duration // default: 1s
}

// newTransport builds a pooled *http.Transport with the provided TLS config and
// pool options. A nil tlsCfg means the system certificate pool is used (normal
// behaviour for plain upstream verification).
func newTransport(tlsCfg *tls.Config, opts TransportOptions) *http.Transport {
	maxIdle := opts.MaxIdleConnsPerHost
	if maxIdle <= 0 {
		maxIdle = 10
	}
	idleTimeout := opts.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = 90 * time.Second
	}
	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	tlsHandshake := opts.TLSHandshakeTimeout
	if tlsHandshake <= 0 {
		tlsHandshake = 10 * time.Second
	}
	respHeaderTimeout := opts.ResponseHeaderTimeout
	if respHeaderTimeout <= 0 {
		respHeaderTimeout = 30 * time.Second
	}
	expectContinue := opts.ExpectContinueTimeout
	if expectContinue <= 0 {
		expectContinue = 1 * time.Second
	}

	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}

	return &http.Transport{
		TLSClientConfig:       tlsCfg,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   maxIdle,
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   tlsHandshake,
		ResponseHeaderTimeout: respHeaderTimeout,
		ExpectContinueTimeout: expectContinue,
		DisableKeepAlives:     false,
	}
}
