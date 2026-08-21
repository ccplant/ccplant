package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
)

func TestWorkerLeaseAcquireRenewRelease(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	controller := controllers.NewWorkerControlController(manager, "secret", nil, nil).WithLeases(schedule.NewRedisLeaseClient(redisClient))
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/internal/worker/leases/test", strings.NewReader(body))
		req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(req, rec)
		ctx.SetParamNames("leaseName")
		ctx.SetParamValues("agentapi:leader:default:test")
		require.NoError(t, controller.Lease(ctx))
		return rec
	}

	require.JSONEq(t, `{"acquired":true}`, call(`{"action":"acquire","identity":"one","duration_ms":10000}`).Body.String())
	require.JSONEq(t, `{"acquired":false}`, call(`{"action":"acquire","identity":"two","duration_ms":10000}`).Body.String())
	require.JSONEq(t, `{"acquired":true}`, call(`{"action":"renew","identity":"one","duration_ms":10000}`).Body.String())
	require.JSONEq(t, `{"acquired":false}`, call(`{"action":"release","identity":"two"}`).Body.String())
	require.JSONEq(t, `{"acquired":true}`, call(`{"action":"release","identity":"one"}`).Body.String())
	require.JSONEq(t, `{"acquired":true}`, call(`{"action":"acquire","identity":"two","duration_ms":10000}`).Body.String())
}
