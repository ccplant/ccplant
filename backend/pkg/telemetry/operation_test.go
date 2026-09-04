package telemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

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
