package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type cleanupRouteRepository struct {
	route      *portrepos.SessionRoute
	deletedIDs []string
}

func (r *cleanupRouteRepository) Save(context.Context, *portrepos.SessionRoute) error { return nil }
func (r *cleanupRouteRepository) Get(_ context.Context, id string) (*portrepos.SessionRoute, error) {
	if r.route != nil && r.route.SessionID == id {
		return r.route, nil
	}
	return nil, nil
}
func (r *cleanupRouteRepository) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	if r.route == nil {
		return nil, nil
	}
	return []*portrepos.SessionRoute{r.route}, nil
}
func (r *cleanupRouteRepository) Delete(_ context.Context, id string) error {
	r.deletedIDs = append(r.deletedIDs, id)
	return nil
}

func TestWorkerSessionListMarksOneshotForTTLCleanup(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"oneshot": {
			id:        "oneshot",
			userID:    "alice",
			scope:     entities.ScopeUser,
			tags:      map[string]string{"schedule_id": "schedule-1"},
			status:    "active",
			startedAt: time.Now(),
			updatedAt: time.Now(),
			request:   &entities.RunServerRequest{Oneshot: true},
		},
	}}
	controller := controllers.NewWorkerControlController(manager, "secret", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/internal/worker/sessions", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	rec := httptest.NewRecorder()

	require.NoError(t, controller.ListSessions(echo.New().NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)
	var sessions []struct {
		Tags map[string]string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sessions))
	require.Len(t, sessions, 1)
	require.Equal(t, "true", sessions[0].Tags["oneshot"])
	require.Equal(t, "1m", sessions[0].Tags["session_ttl"])
}

func TestWorkerDeleteSessionRemovesPoolRouteAfterRuntime(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	routes := &cleanupRouteRepository{route: &portrepos.SessionRoute{
		SessionID:       "public-session",
		RemoteSessionID: "runtime-session",
	}}
	controller := controllers.NewWorkerControlController(manager, "secret", nil, routes)
	req := httptest.NewRequest(http.MethodDelete, "/internal/worker/sessions/public-session", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("public-session")

	require.NoError(t, controller.DeleteSession(ctx))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{"runtime-session"}, manager.deletedIDs)
	require.Equal(t, []string{"public-session"}, routes.deletedIDs)
}

func TestWorkerSessionListIncludesDirectRuntimeOneshotRoute(t *testing.T) {
	startedAt := time.Now().Add(-2 * time.Minute)
	routes := &cleanupRouteRepository{route: &portrepos.SessionRoute{
		SessionID: "public-session", RemoteSessionID: "runtime-session",
		Transport: portrepos.SessionRouteTransportDirectRuntime, UserID: "alice",
		Scope: string(entities.ScopeUser), StartedAt: startedAt, Status: "stopped",
		Tags: map[string]string{"oneshot": "true", "session_ttl": "1m"},
	}}
	controller := controllers.NewWorkerControlController(&fakeSessionManager{sessions: map[string]*fakeSession{}}, "secret", nil, routes)
	req := httptest.NewRequest(http.MethodGet, "/internal/worker/sessions", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	rec := httptest.NewRecorder()

	require.NoError(t, controller.ListSessions(echo.New().NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)
	var sessions []struct {
		ID     string            `json:"id"`
		Status string            `json:"status"`
		Tags   map[string]string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &sessions))
	require.Len(t, sessions, 1)
	require.Equal(t, "public-session", sessions[0].ID)
	require.Equal(t, "stopped", sessions[0].Status)
	require.Equal(t, "1m", sessions[0].Tags["session_ttl"])
}
