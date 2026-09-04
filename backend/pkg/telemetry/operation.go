package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/takutakahashi/agentapi-proxy"

// String creates a string span attribute without exposing the OpenTelemetry SDK
// to instrumented packages.
func String(key, value string) attribute.KeyValue { return attribute.String(key, value) }

// Bool creates a boolean span attribute without exposing the OpenTelemetry SDK
// to instrumented packages.
func Bool(key string, value bool) attribute.KeyValue { return attribute.Bool(key, value) }

// Operation runs fn in an internal span and applies the project's error
// recording policy. It keeps OpenTelemetry lifecycle bookkeeping out of
// application code while preserving context propagation.
func Operation[T any](ctx context.Context, name string, fn func(context.Context) (T, error), attrs ...attribute.KeyValue) (T, error) {
	ctx, span := otel.Tracer(instrumentationName).Start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs...))
	defer span.End()

	result, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return result, err
}
