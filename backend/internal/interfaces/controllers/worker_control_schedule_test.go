package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

type fakeDueScheduleProcessor struct {
	processed int
	called    bool
}

func (p *fakeDueScheduleProcessor) ProcessDueSchedules(context.Context) (int, error) {
	p.called = true
	return p.processed, nil
}

func TestWorkerControlProcessesDueSchedules(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	processor := &fakeDueScheduleProcessor{processed: 2}
	controller := controllers.NewWorkerControlController(manager, "secret", nil, nil).WithScheduleProcessor(processor)
	req := httptest.NewRequest(http.MethodPost, "/internal/worker/schedules/process-due", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer secret")
	rec := httptest.NewRecorder()

	require.NoError(t, controller.ProcessDueSchedules(echo.New().NewContext(req, rec)))
	require.True(t, processor.called)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"processed":2}`, rec.Body.String())
}

func TestWorkerControlRejectsUnauthorizedScheduleProcessing(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	processor := &fakeDueScheduleProcessor{processed: 2}
	controller := controllers.NewWorkerControlController(manager, "secret", nil, nil).WithScheduleProcessor(processor)
	req := httptest.NewRequest(http.MethodPost, "/internal/worker/schedules/process-due", nil)
	rec := httptest.NewRecorder()

	require.NoError(t, controller.ProcessDueSchedules(echo.New().NewContext(req, rec)))
	require.False(t, processor.called)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
