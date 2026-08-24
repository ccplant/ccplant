package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckIncludesReleaseVersion(t *testing.T) {
	t.Setenv("AGENTAPI_VERSION", "v1.4.0")
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/health", nil), recorder)

	require.NoError(t, NewHealthController().HealthCheck(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"status":"ok","version":"v1.4.0"}`, recorder.Body.String())
}
