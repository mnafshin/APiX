package plugins

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// RequestPlugin is implemented by plugins that intercept outbound requests.
// Plugins that only care about requests may implement this alone.
type RequestPlugin interface {
	// OnRequest is called before the request is forwarded upstream.
	// Returning a non-nil *ProxyRequest replaces the request.
	// Returning nil passes the original through unchanged.
	// Returning an error aborts the request (client receives 502).
	OnRequest(ctx context.Context, req *ProxyRequest) (*ProxyRequest, error)
}

// ResponsePlugin is implemented by plugins that intercept responses.
// Plugins that only care about responses may implement this alone.
type ResponsePlugin interface {
	// OnResponse is called after receiving the upstream response.
	// Returning a non-nil *ProxyResponse replaces the response.
	// Returning nil passes the original through unchanged.
	OnResponse(ctx context.Context, req *ProxyRequest, resp *ProxyResponse) (*ProxyResponse, error)
}

// Plugin is the interface every APiX plugin must implement.
// Plugins are called in registration order for every proxied request/response.
// Implementations that only care about one direction may omit the other hook
// by satisfying only RequestPlugin or ResponsePlugin; the runtime skips hooks
// that are not implemented.
type Plugin interface {
	// Name returns the unique plugin identifier (e.g. "header-editor").
	Name() string
	// Version returns the semantic version string (e.g. "1.0.0").
	Version() string
	// Description returns a short human-readable description.
	Description() string
	RequestPlugin
	ResponsePlugin
}

// ProxyRequest is the richer internal request type passed through the plugin chain.
// It is intentionally separate from the proto-generated HttpRequest to allow
// streaming bodies and rich Go types.
type ProxyRequest struct {
	ID      string
	Method  string
	URL     *url.URL
	Headers http.Header
	Body    io.ReadCloser
	// Protocol is the negotiated HTTP protocol: "HTTP/1.1", "HTTP/2.0", or "h2c".
	Protocol string
	// Raw stores the original unmodified request for reference.
	Raw *http.Request
	// MockedResponse, if non-nil, causes the proxy to skip upstream forwarding
	// and return this synthetic response to the client.
	MockedResponse *ProxyResponse
}

// ProxyResponse is the richer internal response type passed through the plugin chain.
type ProxyResponse struct {
	StatusCode int
	Status     string
	Headers    http.Header
	Body       io.ReadCloser
	// Raw stores the original unmodified response for reference.
	Raw *http.Response
}

// Clone returns a shallow copy of the ProxyRequest with a new body reader.
// Useful when a plugin needs to read and replace the body.
func (r *ProxyRequest) Clone(newBody io.ReadCloser) *ProxyRequest {
	clone := *r
	clone.Headers = r.Headers.Clone()
	clone.Body = newBody
	return &clone
}

// Clone returns a shallow copy of the ProxyResponse with a new body reader.
func (r *ProxyResponse) Clone(newBody io.ReadCloser) *ProxyResponse {
	clone := *r
	clone.Headers = r.Headers.Clone()
	clone.Body = newBody
	return &clone
}
