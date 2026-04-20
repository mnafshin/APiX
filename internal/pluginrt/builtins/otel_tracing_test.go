package builtins

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/mnafshin/apix/pkg/plugins"
)

// newNoopOTelTracing builds an OTelTracing that uses a no-op tracer so no
// real OTLP connection is required during unit tests.
func newNoopOTelTracing() *OTelTracing {
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return &OTelTracing{
		cfg:    OTelTracingConfig{},
		tracer: provider.Tracer("apix-test"),
	}
}

func TestOTelTracing_ImplementsPlugin(t *testing.T) {
	t.Parallel()
	var _ plugins.Plugin = newNoopOTelTracing()
}

func TestOTelTracing_Metadata(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	if p.Name() != "otel-tracing" {
		t.Errorf("Name: got %q want %q", p.Name(), "otel-tracing")
	}
	if p.Version() != "1.0.0" {
		t.Errorf("Version: got %q want %q", p.Version(), "1.0.0")
	}
	if p.Description() == "" {
		t.Error("Description: expected non-empty string")
	}
}

func TestOTelTracing_OnRequest_StoresSpan(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("GET", "https://example.com/api/resource", "")
	req.ID = "req-store-test"

	result, err := p.OnRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	if result != nil {
		t.Errorf("OnRequest should return nil (pass-through); got %+v", result)
	}
	if _, ok := p.spans.Load("req-store-test"); !ok {
		t.Error("expected span to be stored in sync.Map after OnRequest")
	}
	if req.Headers.Get("traceparent") == "" {
		t.Fatal("expected traceparent header to be injected on request")
	}
}

func TestOTelTracing_OnRequest_ExtractsParentTraceContext(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("GET", "https://example.com/trace", "")
	req.ID = "req-parent-trace"
	req.Headers.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	if _, err := p.OnRequest(context.Background(), req); err != nil {
		t.Fatalf("OnRequest: %v", err)
	}
	val, ok := p.spans.Load("req-parent-trace")
	if !ok {
		t.Fatal("expected span to be stored")
	}
	_ = val
	traceparent := req.Headers.Get("traceparent")
	if !strings.Contains(traceparent, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("expected injected traceparent to keep parent trace ID, got: %s", traceparent)
	}
}

func TestOTelTracing_OnResponse_RemovesSpan(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("POST", "https://api.example.com/data", "body")
	req.ID = "req-remove-test"

	if _, err := p.OnRequest(context.Background(), req); err != nil {
		t.Fatalf("OnRequest: %v", err)
	}

	resp := makeResp(200, "ok")
	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("OnResponse: %v", err)
	}
	if result != nil {
		t.Errorf("OnResponse should return nil (pass-through); got %+v", result)
	}
	if _, ok := p.spans.Load("req-remove-test"); ok {
		t.Error("expected span to be removed from sync.Map after OnResponse")
	}
}

func TestOTelTracing_OnResponse_NilResponse_NoPanic(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("GET", "https://example.com/", "")
	req.ID = "req-nil-resp-test"

	if _, err := p.OnRequest(context.Background(), req); err != nil {
		t.Fatalf("OnRequest: %v", err)
	}

	// Must not panic when resp is nil.
	result, err := p.OnResponse(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("OnResponse with nil resp: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for nil response, got %+v", result)
	}
}

func TestOTelTracing_OnResponse_UnknownID_NoPanic(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("GET", "https://example.com/", "")
	req.ID = "nonexistent-id"

	resp := makeResp(404, "not found")
	result, err := p.OnResponse(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error for unknown request ID: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for unknown ID, got %+v", result)
	}
}

func TestOTelTracing_OnResponse_ServerError_NoSpanPanic(t *testing.T) {
	t.Parallel()
	p := newNoopOTelTracing()
	req := makeReq("GET", "https://example.com/error", "")
	req.ID = "req-server-error-test"

	if _, err := p.OnRequest(context.Background(), req); err != nil {
		t.Fatalf("OnRequest: %v", err)
	}

	resp := makeResp(503, "service unavailable")
	if _, err := p.OnResponse(context.Background(), req, resp); err != nil {
		t.Fatalf("OnResponse with 503: %v", err)
	}
}

func TestOTelTracingConfig_Defaults(t *testing.T) {
	t.Parallel()
	cfg := OTelTracingConfig{}
	if cfg.endpoint() != "localhost:4317" {
		t.Errorf("endpoint default: got %q", cfg.endpoint())
	}
	if cfg.serviceName() != "apix-proxy" {
		t.Errorf("serviceName default: got %q", cfg.serviceName())
	}
	if cfg.sampleRate() != 1.0 {
		t.Errorf("sampleRate default: got %f", cfg.sampleRate())
	}
}
