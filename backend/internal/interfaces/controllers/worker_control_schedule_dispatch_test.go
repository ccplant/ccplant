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
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWorkerClaimsAndFinalizesScheduleJob(t *testing.T) {
	ctx := context.Background()
	manager := schedule.NewKubernetesManager(fake.NewSimpleClientset(), "default")
	due := time.Now().Add(-time.Minute)
	require.NoError(t, manager.Create(ctx, &schedule.Schedule{ID: "schedule-1", Name: "test", UserID: "alice", Status: schedule.ScheduleStatusActive, ScheduledAt: &due, NextExecutionAt: &due, SessionConfig: schedule.SessionConfig{}}))
	controller := controllers.NewWorkerControlController(&fakeSessionManager{sessions: map[string]*fakeSession{}}, "secret", nil, nil).WithScheduleManager(manager)
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
		} `json:"jobs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Jobs, 1)
	claims, err := executiontoken.VerifyExecutionToken([]byte("secret"), response.Jobs[0].ExecutionToken, time.Now())
	require.NoError(t, err)
	require.Equal(t, "alice", claims.UserID)
	body, _ := json.Marshal(map[string]string{"execution_id": response.Jobs[0].ExecutionID, "session_id": response.Jobs[0].SessionID, "status": "success"})
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
}
