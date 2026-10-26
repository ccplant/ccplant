package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestLoggedOperationOnlyLogsFailures(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	_, err := LoggedOperation(context.Background(), "successful.operation", func(context.Context) (string, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Empty(t, output.String())

	wantErr := errors.New("failed")
	_, err = LoggedOperation(context.Background(), "failed.operation", func(context.Context) (string, error) {
		return "", wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.Contains(t, output.String(), "[OTEL_TIMING] completed operation=failed.operation status=error")
}

func TestOperationRecordsResultAttributesAndError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	wantErr := errors.New("launch failed")
	result, err := Operation(context.Background(), "session.LaunchUseCase.Launch", func(ctx context.Context) (string, error) {
		require.True(t, trace.SpanContextFromContext(ctx).TraceID().IsValid())
		return "partial", wantErr
	}, attribute.String("session.scope", "team"))

	require.Equal(t, "partial", result)
	require.ErrorIs(t, err, wantErr)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "session.LaunchUseCase.Launch", spans[0].Name())
	require.Contains(t, spans[0].Attributes(), attribute.String("session.scope", "team"))
	require.Equal(t, "Error", spans[0].Status().Code.String())
	require.Len(t, spans[0].Events(), 1)
}

func TestHTTPTraceContextRoundTrip(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	sourceCtx, sourceSpan := provider.Tracer("test").Start(context.Background(), "source")
	header := make(http.Header)
	InjectHTTP(sourceCtx, &http.Request{Header: header})
	extractedCtx := ExtractHTTP(context.Background(), header)

	require.Equal(t, sourceSpan.SpanContext().TraceID(), trace.SpanContextFromContext(extractedCtx).TraceID())
	require.Equal(t, sourceSpan.SpanContext().SpanID(), trace.SpanContextFromContext(extractedCtx).SpanID())
	sourceSpan.End()
}
