package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCloudflareTraceAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Header.Set("CF-Ray", "8abc123def456789-nrt")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := cloudflareTraceAttributes(func(echo.Context) error { return nil })
	if err := handler(c); err != nil {
		t.Fatalf("cloudflareTraceAttributes() error = %v", err)
	}
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	attributes := spans[0].Attributes()
	want := map[string]string{
		"cloudflare.ray_id": "8abc123def456789-nrt",
		"cloudflare.colo":   "NRT",
	}
	for key, value := range want {
		found := false
		for _, attr := range attributes {
			if string(attr.Key) == key && attr.Value.AsString() == value {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("attribute %s = %q not found in %v", key, value, attributes)
		}
	}
}

func TestCloudflareTraceAttributesWithoutRay(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	called := false

	handler := cloudflareTraceAttributes(func(echo.Context) error {
		called = true
		return nil
	})
	if err := handler(c); err != nil {
		t.Fatalf("cloudflareTraceAttributes() error = %v", err)
	}
	if !called {
		t.Fatal("next handler was not called")
	}
}
