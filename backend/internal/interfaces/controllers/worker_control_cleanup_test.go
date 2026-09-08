package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

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
