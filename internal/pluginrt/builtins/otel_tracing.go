package builtins

import (
	"context"
	"net/http"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mnafshin/apix/pkg/plugins"
)

// OTelTracingConfig holds the configuration for the OTelTracing plugin.
type OTelTracingConfig struct {
	// Endpoint is the OTLP gRPC endpoint (default: "localhost:4317").
	Endpoint string
	// ServiceName is the OpenTelemetry resource service.name (default: "apix-proxy").
	ServiceName string
	// Insecure uses an insecure gRPC connection when true (default: true for dev).
	Insecure bool
	// SampleRate controls the fraction of traces sampled: 0.0–1.0 (default: 1.0).
	SampleRate float64
}

func (c *OTelTracingConfig) endpoint() string {
	if c.Endpoint == "" {
		return "localhost:4317"
	}
	return c.Endpoint
}

func (c *OTelTracingConfig) serviceName() string {
	if c.ServiceName == "" {
		return "apix-proxy"
	}
	return c.ServiceName
}

func (c *OTelTracingConfig) sampleRate() float64 {
	if c.SampleRate <= 0 {
		return 1.0
	}
	return c.SampleRate
}

// OTelTracing exports a span for every proxied request via OTLP gRPC.
type OTelTracing struct {
	cfg      OTelTracingConfig
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	spans    sync.Map // request ID → trace.Span
}

func (p *OTelTracing) Name() string        { return "otel-tracing" }
func (p *OTelTracing) Version() string     { return "1.0.0" }
func (p *OTelTracing) Description() string { return "Exports request/response spans via OTLP gRPC." }

// NewOTelTracing creates and configures an OTelTracing plugin. Callers must
// call Shutdown when the plugin is no longer needed to flush pending spans.
func NewOTelTracing(ctx context.Context, cfg OTelTracingConfig) (*OTelTracing, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.endpoint()),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(cfg.serviceName())),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.sampleRate())),
	)
	otel.SetTracerProvider(provider)

	return &OTelTracing{
		cfg:      cfg,
		provider: provider,
		tracer:   provider.Tracer("apix"),
	}, nil
}

// Shutdown flushes pending spans and shuts down the provider.
func (p *OTelTracing) Shutdown(ctx context.Context) error {
	return p.provider.Shutdown(ctx)
}

// OnRequest starts a span for the incoming request and stores it keyed by ID.
func (p *OTelTracing) OnRequest(ctx context.Context, req *plugins.ProxyRequest) (*plugins.ProxyRequest, error) {
	spanName := req.Method + " " + req.URL.Path
	_, span := p.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFullKey.String(req.URL.String()),
		semconv.ServerAddressKey.String(req.URL.Host),
	)
	p.spans.Store(req.ID, span)
	return nil, nil
}

// OnResponse ends the stored span and records the response status.
func (p *OTelTracing) OnResponse(ctx context.Context, req *plugins.ProxyRequest, resp *plugins.ProxyResponse) (*plugins.ProxyResponse, error) {
	val, ok := p.spans.LoadAndDelete(req.ID)
	if !ok {
		return nil, nil
	}
	span := val.(trace.Span)
	defer span.End()

	if resp == nil {
		return nil, nil
	}

	span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(resp.StatusCode))
	if resp.StatusCode >= 500 {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}
	return nil, nil
}
