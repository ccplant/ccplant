package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkerLeaseAcquireRenewRelease(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	controller := controllers.NewWorkerControlController(manager, "secret", nil, nil).WithLeases(fake.NewSimpleClientset(), "default")
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
