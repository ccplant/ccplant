package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type operationalSessionManager struct {
	repositories.SessionManager
	tail      int
	sessionID string
}

func (m *operationalSessionManager) OperationalLogs(_ context.Context, _ string, sessionID string, tail int) ([]string, string, error) {
	m.tail, m.sessionID = tail, sessionID
	return []string{"first", "second"}, "pod/session/agentapi", nil
}

func TestGetSessionLogsChecksOwnershipAndReadsPodTail(t *testing.T) {
	base := &fakeSessionManager{sessions: map[string]*fakeSession{
		"session-a": {id: "session-a", userID: "user-1", scope: entities.ScopeUser},
	}}
	manager := &operationalSessionManager{SessionManager: base}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)

	req := httptest.NewRequest(http.MethodGet, "/sessions/session-a/logs?tail=37", nil)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.SetPath("/sessions/:sessionId/logs")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-a")
	ctx.Set("authz_context", &auth.AuthorizationContext{PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true}})

	if err := controller.GetSessionLogs(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"first"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if manager.sessionID != "session-a" || manager.tail != 37 {
		t.Fatalf("session=%q tail=%d", manager.sessionID, manager.tail)
	}

	deniedReq := httptest.NewRequest(http.MethodGet, "/sessions/session-a/logs", nil)
	deniedCtx := echo.New().NewContext(deniedReq, httptest.NewRecorder())
	deniedCtx.SetParamNames("sessionId")
	deniedCtx.SetParamValues("session-a")
	deniedCtx.Set("authz_context", &auth.AuthorizationContext{PersonalScope: auth.PersonalScopeAuth{UserID: "user-2", CanRead: true}})
	err := controller.GetSessionLogs(deniedCtx)
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
