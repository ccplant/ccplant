package controllers_test

import (
	"bytes"
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
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkerClaimsAndFinalizesScheduleJob(t *testing.T) {
	ctx := context.Background()
	manager := schedule.NewKubernetesManager(fake.NewSimpleClientset(), "default")
	due := time.Now().Add(-time.Minute)
	require.NoError(t, manager.Create(ctx, &schedule.Schedule{
		ID: "schedule-1", Name: "test", UserID: "alice", Status: schedule.ScheduleStatusActive,
		ScheduledAt: &due, NextExecutionAt: &due,
		SessionConfig: schedule.SessionConfig{
			Params:       &entities.SessionParams{Message: "initial message"},
			ReuseSession: true,
		},
	}))
	controller := controllers.NewWorkerControlController(&fakeSessionManager{sessions: map[string]*fakeSession{}}, "secret", "execution-secret", nil, nil).WithScheduleManager(manager)
	req := httptest.NewRequest(http.MethodPost, "/internal/worker/schedules/claim-due", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	rec := httptest.NewRecorder()
	require.NoError(t, controller.ClaimDueSchedules(echo.New().NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		Jobs []struct {
			ScheduleID     string `json:"schedule_id"`
			ExecutionID    string `json:"execution_id"`
			SessionID      string `json:"session_id"`
			ExecutionToken string `json:"execution_token"`
			StartRequest   struct {
				Tags           map[string]string `json:"tags"`
				ReuseMatchTags map[string]string `json:"reuse_match_tags"`
				ReuseMessage   string            `json:"reuse_message"`
			} `json:"start_request"`
		} `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Jobs, 1)
	claims, err := executiontoken.VerifyExecutionToken([]byte("execution-secret"), response.Jobs[0].ExecutionToken, time.Now())
	require.NoError(t, err)
	_, err = executiontoken.VerifyExecutionToken([]byte("secret"), response.Jobs[0].ExecutionToken, time.Now())
	require.Error(t, err, "worker bearer credential must not verify execution tokens")
	require.Equal(t, "alice", claims.UserID)
	require.Equal(t, map[string]string{"schedule_id": "schedule-1", "schedule_name": "test"}, response.Jobs[0].StartRequest.Tags)
	require.Equal(t, response.Jobs[0].StartRequest.Tags, response.Jobs[0].StartRequest.ReuseMatchTags)
	require.Equal(t, "initial message", response.Jobs[0].StartRequest.ReuseMessage)
	body, _ := json.Marshal(map[string]any{"execution_id": response.Jobs[0].ExecutionID, "session_id": response.Jobs[0].SessionID, "status": "success", "session_reused": true})
	req = httptest.NewRequest(http.MethodPost, "/internal/worker/schedules/schedule-1/finalize", bytes.NewReader(body))
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ec := echo.New().NewContext(req, httptest.NewRecorder())
	ec.SetParamNames("id")
	ec.SetParamValues("schedule-1")
	require.NoError(t, controller.FinalizeSchedule(ec))
	got, err := manager.Get(ctx, "schedule-1")
	require.NoError(t, err)
	require.Equal(t, 1, got.ExecutionCount)
	require.Equal(t, schedule.ScheduleStatusCompleted, got.Status)
	require.True(t, got.LastExecution.SessionReused)
}
