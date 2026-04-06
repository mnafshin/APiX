package pluginrt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mnafshin/apix/pkg/plugins"
)

// mockPlugin is a test double implementing the Plugin interface.
type mockPlugin struct {
	name         string
	onRequestFn  func(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error)
	onResponseFn func(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error)
}

func (p *mockPlugin) Name() string        { return p.name }
func (p *mockPlugin) Version() string     { return "0.0.1" }
func (p *mockPlugin) Description() string { return "mock plugin for testing" }

func (p *mockPlugin) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	if p.onRequestFn != nil {
		return p.onRequestFn(ctx, req)
	}
	return nil, nil
}

func (p *mockPlugin) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	if p.onResponseFn != nil {
		return p.onResponseFn(ctx, req, resp)
	}
	return nil, nil
}

func newProxyRequest(method, rawURL string) *plugins.ProxyRequest {
	u, _ := url.Parse(rawURL)
	return &plugins.ProxyRequest{
		ID:      "test-id",
		Method:  method,
		URL:     u,
		Headers: make(http.Header),
		Body:    io.NopCloser(strings.NewReader("")),
	}
}

func newProxyResponse(statusCode int) *plugins.ProxyResponse {
	return &plugins.ProxyResponse{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Headers:    make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func TestRegisterAndList(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()
	p := &mockPlugin{name: "test-plugin"}
	if err := rt.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list := rt.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if list[0].Name != "test-plugin" {
		t.Errorf("Name: got %q", list[0].Name)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()
	p := &mockPlugin{name: "dup-plugin"}
	if err := rt.Register(p); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := rt.Register(p); err == nil {
		t.Error("expected error on duplicate registration, got nil")
	}
}

func TestUnregister(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()
	p := &mockPlugin{name: "removable"}
	if err := rt.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := rt.Unregister("removable"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if len(rt.List()) != 0 {
		t.Error("expected 0 plugins after unregister")
	}
}

func TestRunRequestChain(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()

	p1 := &mockPlugin{
		name: "plugin-1",
		onRequestFn: func(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
			clone := req.Clone(req.Body)
			clone.Headers.Set("X-Plugin-1", "applied")
			return clone, nil
		},
	}
	p2 := &mockPlugin{
		name: "plugin-2",
		onRequestFn: func(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
			clone := req.Clone(req.Body)
			clone.Headers.Set("X-Plugin-2", "applied")
			return clone, nil
		},
	}

	if err := rt.Register(p1); err != nil {
		t.Fatalf("Register p1: %v", err)
	}
	if err := rt.Register(p2); err != nil {
		t.Fatalf("Register p2: %v", err)
	}

	req := newProxyRequest("GET", "https://example.com")
	result, err := rt.RunRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("RunRequest: %v", err)
	}

	if result.Headers.Get("X-Plugin-1") != "applied" {
		t.Error("plugin-1 header not applied")
	}
	if result.Headers.Get("X-Plugin-2") != "applied" {
		t.Error("plugin-2 header not applied")
	}
}

func TestRunRequestShortCircuit(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()

	p1 := &mockPlugin{
		name: "erroring-plugin",
		onRequestFn: func(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
			return nil, errors.New("plugin error")
		},
	}
	p2 := &mockPlugin{
		name: "unreachable-plugin",
		onRequestFn: func(_ context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
			req.Headers.Set("X-Unreachable", "true")
			return req, nil
		},
	}

	if err := rt.Register(p1); err != nil {
		t.Fatalf("Register p1: %v", err)
	}
	if err := rt.Register(p2); err != nil {
		t.Fatalf("Register p2: %v", err)
	}

	req := newProxyRequest("GET", "https://example.com")
	_, err := rt.RunRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from erroring plugin, got nil")
	}
	if !strings.Contains(err.Error(), "plugin error") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunResponseChain(t *testing.T) {
	t.Parallel()
	rt := NewRuntime()

	p1 := &mockPlugin{
		name: "resp-plugin-1",
		onResponseFn: func(_ context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
			clone := resp.Clone(resp.Body)
			clone.Headers.Set("X-Resp-Plugin-1", "applied")
			return clone, nil
		},
	}
	p2 := &mockPlugin{
		name: "resp-plugin-2",
		onResponseFn: func(_ context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
			clone := resp.Clone(resp.Body)
			clone.Headers.Set("X-Resp-Plugin-2", "applied")
			return clone, nil
		},
	}

	if err := rt.Register(p1); err != nil {
		t.Fatalf("Register p1: %v", err)
	}
	if err := rt.Register(p2); err != nil {
		t.Fatalf("Register p2: %v", err)
	}

	req := newProxyRequest("GET", "https://example.com")
	resp := newProxyResponse(200)
	result, err := rt.RunResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("RunResponse: %v", err)
	}

	if result.Headers.Get("X-Resp-Plugin-1") != "applied" {
		t.Error("resp-plugin-1 header not applied")
	}
	if result.Headers.Get("X-Resp-Plugin-2") != "applied" {
		t.Error("resp-plugin-2 header not applied")
	}
}
