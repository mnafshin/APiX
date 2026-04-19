package replay

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/storage"
)

// ClientConfig holds options for the replay HTTP client.
// It exposes fine-grained upstream timeouts so the replay engine can be
// configured to fail-fast against slow or unresponsive upstreams.
type ClientConfig struct {
	SkipTLSVerify         bool
	Client                *http.Client
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	MaxIdleConnsPerHost   int
}

// Engine replays stored or user-supplied HTTP requests against the original
// (or a configured) upstream host.
type Engine struct {
	db       *storage.DB
	client   *http.Client
	builders []ReplaySourceBuilder
}

// NewEngine creates a replay engine.
// If cfg is nil, uses system certificate pool (TLS verification enabled).
// Accepts optional custom http.Client or ClientConfig.SkipTLSVerify to bypass verification.
func NewEngine(db *storage.DB, cfg *ClientConfig) *Engine {
	if cfg != nil && cfg.Client != nil {
		e := &Engine{
			db:     db,
			client: cfg.Client,
			builders: []ReplaySourceBuilder{
				&storedRequestBuilder{db: db},
				&rawRequestBuilder{},
			},
		}
		return e
	}

	// Defaults mirror proxy/transport defaults.
	dialTimeout := 10 * time.Second
	idleTimeout := 90 * time.Second
	tlsHandshake := 10 * time.Second
	respHeaderTimeout := 30 * time.Second
	expectContinue := 1 * time.Second
	maxIdle := 10

	if cfg != nil {
		if cfg.DialTimeout != 0 {
			dialTimeout = cfg.DialTimeout
		}
		if cfg.IdleConnTimeout != 0 {
			idleTimeout = cfg.IdleConnTimeout
		}
		if cfg.TLSHandshakeTimeout != 0 {
			tlsHandshake = cfg.TLSHandshakeTimeout
		}
		if cfg.ResponseHeaderTimeout != 0 {
			respHeaderTimeout = cfg.ResponseHeaderTimeout
		}
		if cfg.ExpectContinueTimeout != 0 {
			expectContinue = cfg.ExpectContinueTimeout
		}
		if cfg.MaxIdleConnsPerHost != 0 {
			maxIdle = cfg.MaxIdleConnsPerHost
		}
	}

	// Build transport with dialer so Dial timeout is honoured.
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg != nil && cfg.SkipTLSVerify {
		tlsCfg = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec
		}
	}

	transport := &http.Transport{
		TLSClientConfig:       tlsCfg,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   maxIdle,
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   tlsHandshake,
		ResponseHeaderTimeout: respHeaderTimeout,
		ExpectContinueTimeout: expectContinue,
	}

	client := &http.Client{Transport: transport}
	return &Engine{
		db:     db,
		client: client,
		builders: []ReplaySourceBuilder{
			&storedRequestBuilder{db: db},
			&rawRequestBuilder{},
		},
	}
}

// ReplayRequest sends req (with optional header/body overrides) and returns
// the HTTP response.
func (e *Engine) ReplayRequest(ctx context.Context, req *ReplayRequest) (*http.Response, error) {
	var httpReq *http.Request

	var built bool
	for _, b := range e.builders {
		if b.CanHandle(req) {
			var err error
			httpReq, err = b.Build(ctx, req)
			if err != nil {
				return nil, err
			}
			built = true
			break
		}
	}
	if !built {
		return nil, fmt.Errorf("replay: either RequestID or RawRequest must be set")
	}

	// Apply header overrides.
	httputil.SetValidHeaders(ctx, httpReq.Header, req.OverrideHeaders, "replay")

	// Apply method override.
	if req.OverrideMethod != "" {
		httpReq.Method = req.OverrideMethod
	}

	// Handle redirect policy.
	client := e.client
	if !req.FollowRedirects {
		// Wrap the client with a no-redirect policy without mutating the shared one.
		noRedirectClient := *client
		noRedirectClient.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
		client = &noRedirectClient
	}

	return client.Do(httpReq)
}
