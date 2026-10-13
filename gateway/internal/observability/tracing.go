package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// OTLPSettings is deliberately small: credentials belong to the collector
// network policy, not in a runtime document, trace attribute, or log line.
type OTLPSettings struct {
	Enabled       bool
	Endpoint      string
	Protocol      string
	Insecure      bool
	SampleRatio   float64
	ExportTimeout time.Duration
}

// Tracing owns the optional OTLP SDK. It does not install a global provider so
// independently constructed handlers and tests cannot leak spans into Yaginx.
type Tracing struct {
	provider   *sdktrace.TracerProvider
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

func NewTracing(ctx context.Context, settings OTLPSettings, version, commit string) (*Tracing, error) {
	if !settings.Enabled {
		return &Tracing{}, nil
	}
	if settings.Endpoint == "" {
		return nil, fmt.Errorf("OTLP endpoint is required when tracing is enabled")
	}
	if settings.ExportTimeout <= 0 {
		settings.ExportTimeout = 5 * time.Second
	}
	if settings.SampleRatio < 0 || settings.SampleRatio > 1 {
		return nil, fmt.Errorf("OTLP sample ratio must be between 0 and 1")
	}
	var exporter sdktrace.SpanExporter
	var err error
	switch settings.Protocol {
	case "grpc":
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(settings.Endpoint), otlptracegrpc.WithTimeout(settings.ExportTimeout)}
		if settings.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	case "http/protobuf":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(settings.Endpoint), otlptracehttp.WithTimeout(settings.ExportTimeout)}
		if settings.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", settings.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}
	service := resource.NewWithAttributes("",
		attribute.String("service.name", "yaginx"),
		attribute.String("service.version", version),
		attribute.String("service.git_sha", commit),
	)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(service),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(settings.SampleRatio))),
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(settings.ExportTimeout)),
	)
	return &Tracing{provider: provider, tracer: provider.Tracer("yaginx-gateway"), propagator: propagation.TraceContext{}}, nil
}

func (t *Tracing) Shutdown(ctx context.Context) error {
	if t == nil || t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// Handler records only stable, non-sensitive request facts. In particular it
// intentionally omits Host, raw path, query, headers, body, account and token.
func (t *Tracing) Handler(listener string, next http.Handler) http.Handler {
	if t == nil || t.provider == nil || next == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := t.propagator.Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		ctx, span := t.tracer.Start(ctx, "yaginx.request", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		span.SetAttributes(
			attribute.String("yaginx.listener", listener),
			attribute.String("http.request.method", traceMethod(request.Method)),
			attribute.String("network.protocol.version", request.Proto),
		)
		next.ServeHTTP(w, request.WithContext(ctx))
	})
}

func traceMethod(value string) string {
	switch value {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead:
		return value
	default:
		return "OTHER"
	}
}
