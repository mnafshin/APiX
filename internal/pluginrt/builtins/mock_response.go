package builtins

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"

	"github.com/mnafshin/apix/pkg/plugins"
)

// MockResponse short-circuits the proxy and returns a synthetic response
// for requests matching a URL pattern.
type MockResponse struct {
	URLPattern string
	StatusCode int
	Headers    map[string]string
	Body       []byte

	compiled *regexp.Regexp
}

func (p *MockResponse) Name() string        { return "mock-response" }
func (p *MockResponse) Version() string     { return "1.0.0" }
func (p *MockResponse) Description() string { return "Return a synthetic response for matched URLs." }

func (p *MockResponse) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if p.URLPattern == "" {
		return nil, nil
	}
	if p.compiled == nil {
		re, err := regexp.Compile(p.URLPattern)
		if err != nil {
			return nil, err
		}
		p.compiled = re
	}
	if !p.compiled.MatchString(req.URL.String()) {
		return nil, nil
	}

	statusCode := p.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	hdrs := make(http.Header)
	for k, v := range p.Headers {
		hdrs.Set(k, v)
	}

	body := p.Body
	if body == nil {
		body = []byte{}
	}

	// Clone the request and attach the synthetic response.
	clone := req.Clone(req.Body)
	clone.MockedResponse = &plugins.ProxyResponse{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Headers:    hdrs,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	return clone, nil
}

func (p *MockResponse) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	return nil, nil
}
