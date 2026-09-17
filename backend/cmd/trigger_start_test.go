package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/webhook"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAPITriggersUseStartEndpoint(t *testing.T) {
	var starts []entities.StartRequest
	var origins []executiontoken.ExecutionClaims
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/internal/worker/sessions" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/start" {
			t.Errorf("trigger bypassed /start: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		claims, err := executiontoken.VerifyExecutionToken([]byte("secret"), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var start entities.StartRequest
		if err = json.NewDecoder(r.Body).Decode(&start); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		starts = append(starts, start)
		origins = append(origins, claims)
		_ = json.NewEncoder(w).Encode(map[string]string{"session_id": claims.SessionID})
	}))
	defer api.Close()
	u, err := url.Parse(api.URL)
	require.NoError(t, err)
	previousPort := port
	port = u.Port()
	t.Cleanup(func() { port = previousPort })
	cfg := config.DefaultConfig()
	cfg.Worker.ControlAPIToken = "secret"
	manager := newTriggerSessionManager(cfg)

	schedules := schedule.NewKubernetesManager(fake.NewSimpleClientset(), "test")
	due := time.Now().Add(time.Hour)
	require.NoError(t, schedules.Create(context.Background(), &schedule.Schedule{ID: "schedule", Name: "manual smoke", UserID: "owner", Scope: entities.ScopeUser, Status: schedule.ScheduleStatusActive, ScheduledAt: &due, Timezone: "UTC", SessionConfig: schedule.SessionConfig{Params: &entities.SessionParams{Oneshot: true, Message: "finish", SessionTTL: "2m"}}}))
	handler := schedule.NewHandlers(schedules, manager, nil, nil)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/schedules/schedule/trigger", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("schedule")
	c.Set("internal_user", entities.NewUser("owner", entities.UserTypeAPIKey, "owner"))
	require.NoError(t, handler.TriggerSchedule(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, starts, 1)
	require.Equal(t, "schedule", origins[0].ScheduleID)
	require.False(t, starts[0].Params.Oneshot)
	require.Equal(t, "2m", starts[0].Params.SessionTTL)
	var manual map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &manual))
	require.Equal(t, origins[0].SessionID, manual["session_id"])
	stored, err := schedules.Get(context.Background(), "schedule")
	require.NoError(t, err)
	require.Equal(t, origins[0].SessionID, stored.LastExecution.SessionID)

	// GitHub/custom receivers and webhook's manual trigger share this launcher.
	wh := entities.NewWebhook("webhook", "smoke", "owner", entities.WebhookTypeCustom)
	sc := entities.NewWebhookSessionConfig()
	sc.SetParams(&entities.SessionParams{Oneshot: true, SessionTTL: "2m"})
	sc.SetInitialMessageTemplate("finish {{.event}}")
	tr := entities.NewWebhookTrigger("trigger", "smoke")
	tr.SetSessionConfig(sc)
	service := webhook.NewWebhookSessionService(nil, manager, nil, nil)
	payload := []byte(`{"event":"test"}`)
	id, reused, err := service.CreateSessionFromWebhook(context.Background(), webhook.SessionCreationParams{Webhook: wh, Trigger: &tr, Tags: map[string]string{"webhook_id": "webhook", "trigger_id": "trigger"}, Payload: map[string]interface{}{"event": "test"}, RawPayload: payload, MountPayload: true})
	require.NoError(t, err)
	require.False(t, reused)
	require.Len(t, starts, 2)
	require.Equal(t, origins[1].SessionID, id)
	require.Equal(t, "webhook", origins[1].WebhookID)
	require.Equal(t, "owner", origins[1].UserID)
	require.False(t, starts[1].Params.Oneshot)
	require.Equal(t, "2m", starts[1].Params.SessionTTL)
	require.Equal(t, "finish test", starts[1].Params.Message)
	require.Equal(t, payload, starts[1].WebhookPayload)
}
