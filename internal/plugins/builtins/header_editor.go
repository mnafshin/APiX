package builtins

import (
	"bytes"
	"context"
	"io"

	"github.com/mnafshin/apix/internal/plugins"
)

// HeaderEditor adds, removes, or replaces request/response headers.
type HeaderEditor struct {
	// RequestHeaders: header name → value. Empty value means delete the header.
	RequestHeaders map[string]string
	// ResponseHeaders: header name → value. Empty value means delete the header.
	ResponseHeaders map[string]string
}

func (p *HeaderEditor) Name() string        { return "header-editor" }
func (p *HeaderEditor) Version() string     { return "1.0.0" }
func (p *HeaderEditor) Description() string { return "Add, remove, or replace request/response headers." }

func (p *HeaderEditor) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if len(p.RequestHeaders) == 0 {
		return nil, nil
	}
	// Read and re-wrap body so the clone can be read again.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(io.NopCloser(bytes.NewReader(body)))
	for k, v := range p.RequestHeaders {
		if v == "" {
			clone.Headers.Del(k)
		} else {
			clone.Headers.Set(k, v)
		}
	}
	return clone, nil
}

func (p *HeaderEditor) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if len(p.ResponseHeaders) == 0 {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	clone := resp.Clone(io.NopCloser(bytes.NewReader(body)))
	for k, v := range p.ResponseHeaders {
		if v == "" {
			clone.Headers.Del(k)
		} else {
			clone.Headers.Set(k, v)
		}
	}
	return clone, nil
}
