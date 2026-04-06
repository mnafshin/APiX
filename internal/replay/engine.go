package replay

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"github.com/mnafshin/apix/internal/storage"
)

// Engine replays stored or user-supplied HTTP requests against the original
// (or a configured) upstream host.
type Engine struct {
	db     *storage.DB
	client *http.Client
}

// NewEngine creates a replay engine with optional custom http.Client.
// If client is nil, a default client with TLS verification disabled is used
// (appropriate for intercepting-proxy use).
func NewEngine(db *storage.DB, client *http.Client) *Engine {
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	}
	return &Engine{db: db, client: client}
}

// ReplayRequest sends req (with optional header/body overrides) and returns
// the HTTP response.
func (e *Engine) ReplayRequest(ctx context.Context, req *ReplayRequest) (*http.Response, error) {
	var httpReq *http.Request

	switch {
	case req.RequestID != "":
		// Load from storage.
		rec, _, err := e.db.GetTransaction(req.RequestID)
		if err != nil {
			return nil, fmt.Errorf("load request %q: %w", req.RequestID, err)
		}
		if rec == nil {
			return nil, fmt.Errorf("request %q not found", req.RequestID)
		}
		var bodyReader io.Reader
		if len(req.OverrideBody) > 0 {
			bodyReader = bytes.NewReader(req.OverrideBody)
		} else if len(rec.Body) > 0 {
			bodyReader = bytes.NewReader(rec.Body)
		}
		httpReq, err = http.NewRequestWithContext(ctx, rec.Method, rec.URL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		for k, v := range rec.Headers {
			httpReq.Header.Set(k, v)
		}

	case req.RawRequest != nil:
		// Use the supplied request (clone it with our ctx).
		var body io.Reader
		if len(req.OverrideBody) > 0 {
			body = bytes.NewReader(req.OverrideBody)
		} else if req.RawRequest.Body != nil {
			bodyBytes, err := io.ReadAll(req.RawRequest.Body)
			if err != nil {
				return nil, fmt.Errorf("read raw request body: %w", err)
			}
			body = bytes.NewReader(bodyBytes)
		}
		var err error
		httpReq, err = http.NewRequestWithContext(ctx, req.RawRequest.Method, req.RawRequest.URL.String(), body)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header = req.RawRequest.Header.Clone()

	default:
		return nil, fmt.Errorf("replay: either RequestID or RawRequest must be set")
	}

	// Apply header overrides.
	for k, v := range req.OverrideHeaders {
		httpReq.Header.Set(k, v)
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
