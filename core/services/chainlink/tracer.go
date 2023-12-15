package chainlink

import (
	"context"

	"go.opentelemetry.io/otel"
	stdout "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/smartcontractkit/chainlink/v2/core/config"
)

func newExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	exporter, err := stdout.New(stdout.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	return exporter, nil
}

func newTraceProvider(exp sdktrace.SpanExporter) (*sdktrace.TracerProvider, error) {
	// Ensure default SDK resources and the required service name are set.
	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("ExampleService"),
		),
	)

	if err != nil {
		return nil, err
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(r),
	), nil
}

func initTracer(cfg config.Tracing) (tracer trace.Tracer, shutdown func(ctx context.Context) error, err error) {
	/*
		if !cfg.Enabled() {
			return noop.NewTracerProvider().Tracer("noop"), func(context.Context) error { return nil }, nil
		}
	*/

	// TODO: implement the rest cfg handling. for now, just use stdout exporter
	// Create a new stdout exporter
	exporter, err := newExporter(context.Background())
	if err != nil {
		return nil, nil, err
	}

	// Create a new trace provider with a batch span processor and the given exporter.
	tp, err := newTraceProvider(exporter)
	if err != nil {
		return nil, nil, err
	}

	otel.SetTracerProvider(tp)

	// Finally, set the tracer that can be used for this package.
	return tp.Tracer("Chainlink"), tp.Shutdown, nil
}
