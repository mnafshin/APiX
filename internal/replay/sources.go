package replay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	httputil "github.com/mnafshin/apix/internal/http"
	"github.com/mnafshin/apix/internal/storage"
)

// ReplaySourceBuilder constructs an *http.Request from a ReplayRequest.
// Each implementation encapsulates one source type (stored record, raw request, …).
// The Engine iterates its registered builders and delegates to the first one
// whose CanHandle returns true, making it straightforward to add new sources
// without touching existing code.
type ReplaySourceBuilder interface {
	// CanHandle reports whether this builder can satisfy req.
	CanHandle(req *ReplayRequest) bool
	// Build constructs the *http.Request that will be sent upstream.
	Build(ctx context.Context, req *ReplayRequest) (*http.Request, error)
}

// storedRequestBuilder loads a transaction from the storage DB and builds a
// request from the persisted record.
type storedRequestBuilder struct {
	db *storage.DB
}

func (b *storedRequestBuilder) CanHandle(req *ReplayRequest) bool {
	return req.RequestID != ""
}

func (b *storedRequestBuilder) Build(ctx context.Context, req *ReplayRequest) (*http.Request, error) {
	rec, _, err := b.db.GetTransaction(req.RequestID)
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

	httpReq, err := http.NewRequestWithContext(ctx, rec.Method, rec.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httputil.SetValidHeaders(ctx, httpReq.Header, rec.Headers, "replay")
	return httpReq, nil
}

// rawRequestBuilder uses the caller-supplied *http.Request directly.
type rawRequestBuilder struct{}

func (b *rawRequestBuilder) CanHandle(req *ReplayRequest) bool {
	return req.RawRequest != nil
}

func (b *rawRequestBuilder) Build(ctx context.Context, req *ReplayRequest) (*http.Request, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, req.RawRequest.Method, req.RawRequest.URL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header = req.RawRequest.Header.Clone()
	return httpReq, nil
}
