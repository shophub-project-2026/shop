// Package tracing initialises the OpenTelemetry TracerProvider for the Shop
// service and exports spans over OTLP/HTTP to a configurable collector
// (e.g. Jaeger, Grafana Tempo, or any OTLP-compatible backend).
//
// If no endpoint is provided the global no-op provider remains in place so
// the service still compiles and runs without a collector configured.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init configures the global OTel TracerProvider.
//
// endpoint must be a full URL, e.g. "http://localhost:4318".
// If endpoint is empty the function is a no-op and returns a zero-cost
// shutdown function; no network connection is attempted.
//
// The returned shutdown function must be called on service exit to flush
// any buffered spans before the process terminates.
func Init(ctx context.Context, endpoint, serviceName string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.namespace", "shophub-project-2026"),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
