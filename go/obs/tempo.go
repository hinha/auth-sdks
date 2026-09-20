package obs

import (
	"context"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func (h *Handle) Tracer(name string) trace.Tracer {
	if h == nil || h.tp == nil {
		return noop.NewTracerProvider().Tracer(name)
	}
	return h.tp.Tracer(name)
}

// newTracer is the seam New() uses to build the tracer provider, so the
// fail-open path can be exercised without a real exporter failure. Production
// always resolves to newTracerProvider.
var newTracer = newTracerProvider

func newTracerProvider(ctx context.Context, cfg Config) (trace.TracerProvider, func(context.Context) error, error) {
	endpoint := joinAPI(cfg.URL, pathOTLPTraces)
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
	}
	if u, err := url.Parse(endpoint); err == nil && strings.EqualFold(u.Scheme, "http") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	headers := map[string]string{"User-Agent": userAgent}
	if cfg.Username != "" || cfg.Password != "" {
		headers["Authorization"] = basicAuthHeader(cfg.Username, cfg.Password)
	}
	opts = append(opts, otlptracehttp.WithHeaders(headers))
	if cfg.HTTPClient != nil {
		opts = append(opts, otlptracehttp.WithHTTPClient(cfg.HTTPClient))
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, nil, err
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(nonEmpty(cfg.Service, "auth-sdks")),
		),
	)
	if err != nil {
		_ = exp.Shutdown(ctx)
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	if cfg.InstallGlobal {
		otel.SetTracerProvider(tp)
	}
	return tp, tp.Shutdown, nil
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
