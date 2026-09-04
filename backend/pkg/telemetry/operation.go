package telemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/takutakahashi/agentapi-proxy"

// String creates a string span attribute without exposing the OpenTelemetry SDK
// to instrumented packages.
func String(key, value string) attribute.KeyValue { return attribute.String(key, value) }

// Bool creates a boolean span attribute without exposing the OpenTelemetry SDK
// to instrumented packages.
func Bool(key string, value bool) attribute.KeyValue { return attribute.Bool(key, value) }

// InjectHTTP writes the configured trace propagation headers onto req. Use it
// for HTTP-shaped requests sent through a tunnel instead of an http.Transport.
func InjectHTTP(ctx context.Context, req *http.Request) {
	traceContextPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}

// ExtractHTTP restores trace propagation headers from an HTTP-shaped request.
// It is intended for requests carried over transports that bypass net/http's
// server instrumentation, such as the durable ESM control tunnel.
func ExtractHTTP(ctx context.Context, header http.Header) context.Context {
	return traceContextPropagator().Extract(ctx, propagation.HeaderCarrier(header))
}

func traceContextPropagator() propagation.TextMapPropagator {
	// W3C propagation must also work in lightweight processes such as
	// agent-provisioner, where an OTLP exporter is intentionally not configured.
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// Int64 creates an int64 span attribute without exposing the OpenTelemetry SDK
// to instrumented packages.
func Int64(key string, value int64) attribute.KeyValue { return attribute.Int64(key, value) }

// SetAttributes adds attributes to the current span.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

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

// OperationErr is Operation for methods that return only an error.
func OperationErr(ctx context.Context, name string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	_, err := Operation(ctx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, attrs...)
	return err
}

// OperationVoid is Operation for methods that do not return a value.
func OperationVoid(ctx context.Context, name string, fn func(context.Context), attrs ...attribute.KeyValue) {
	_, _ = Operation(ctx, name, func(ctx context.Context) (struct{}, error) {
		fn(ctx)
		return struct{}{}, nil
	}, attrs...)
}
