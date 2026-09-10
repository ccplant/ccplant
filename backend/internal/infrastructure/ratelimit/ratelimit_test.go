package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestLimiter_AllowsWithinWindow(t *testing.T) {
	l := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("4th request should be denied")
	}
	// Independent keys are unaffected.
	if !l.Allow("5.6.7.8") {
		t.Error("different key should be allowed")
	}
}

func TestLimiter_WindowReset(t *testing.T) {
	l := New(1, 10 * time.Millisecond)
	if !l.Allow("ip") {
		t.Fatal("first request should be allowed")
	}
	if l.Allow("ip") {
		t.Error("second request should be denied")
	}
	time.Sleep(15 * time.Millisecond)
	if !l.Allow("ip") {
		t.Error("request after window reset should be allowed")
	}
}

func TestLimiter_Disabled(t *testing.T) {
	l := New(0, time.Minute)
	for i := 0; i < 100; i++ {
		if !l.Allow("ip") {
			t.Fatal("disabled limiter must always allow")
		}
	}
}

func TestEchoMiddleware_Returns429AfterLimit(t *testing.T) {
	l := New(1, time.Minute)
	e := echo.New()
	handler := EchoMiddleware(l, func(c echo.Context) string { return c.Request().Header.Get("X-Forwarded-For") })(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	// First request passes.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := handler(c); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}

	// Second request from the same IP is throttled.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.Header.Set("X-Forwarded-For", "9.9.9.9")
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)
	err := handler(c2)
	if err == nil {
		t.Fatal("second request should be throttled")
	}
	if he, ok := err.(*echo.HTTPError); !ok || he.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %+v", err)
	}
	if got := rec2.Header().Get(echo.HeaderRetryAfter); got == "" {
		t.Error("Retry-After header should be set on throttled responses")
	}
}
