package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/controlapi"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

type slackStartManager struct{ repositories.SessionManager }

func (*slackStartManager) GetSession(string) entities.Session               { return nil }
func (m *slackStartManager) GetSessionManager() repositories.SessionManager { return m }

type slackStartCreator struct {
	SessionCreator
	request entities.StartRequest
	userID  string
}

func (s *slackStartCreator) CreateSession(_ context.Context, id string, req entities.StartRequest, userID, _ string, _ []string) (entities.Session, error) {
	s.request, s.userID = req, userID
	return entities.NewProxySessionWithStatus(id, userID, req.Scope, req.TeamID, req.Tags, time.Now(), "creating"), nil
}

// Exercise the worker HTTP client, normal authentication middleware and actual
// POST /start controller together. The control API must never receive creation.
func TestSlackWorkerUsesNormalStartAPI(t *testing.T) {
	creator := &slackStartCreator{}
	controller := NewSessionController(&slackStartManager{}, creator)
	cfg := &config.Config{}
	cfg.Worker.ControlAPIToken = "worker-secret"
	e := echo.New()
	e.Use(auth.AuthMiddleware(cfg, nil))
	e.POST("/start", controller.StartSession)
	api := httptest.NewServer(e)
	defer api.Close()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected control API request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer control.Close()
	client := controlapi.NewSessionManager(control.URL, "worker-secret").WithSessionAPIURL(api.URL)
	request := &entities.RunServerRequest{
		UserID: "owner", TriggeredUserID: "actor", Scope: entities.ScopeTeam, TeamID: "org/team", Teams: []string{"org/team"},
		Tags: map[string]string{"slackbot_id": "bot", "slack_channel": "channel", "slack_thread_ts": "123.45"},
		Pool: "linux", InitialMessage: "Investigate", AgentType: "codex", Model: "test-model",
		SlackParams: &entities.SlackParams{Channel: "channel", ThreadTS: "123.45", BotTokenSecretName: "custom-bot"},
		RepoInfo:    &entities.RepositoryInfo{FullName: "org/repo"}, ResolvedSessionProfileID: "profile",
		CredentialSource: "triggered_user", CycleMessage: "continue", CycleMaxCount: 3,
	}
	session, err := client.CreateSession(context.Background(), "slack-session", request, nil)
	require.NoError(t, err)
	require.Equal(t, "slack-session", session.ID())
	require.Equal(t, "owner", creator.userID)
	require.Equal(t, "actor", creator.request.TriggeredUserID)
	require.Equal(t, entities.ScopeTeam, creator.request.Scope)
	require.Equal(t, "org/team", creator.request.TeamID)
	require.Equal(t, request.SlackParams, creator.request.Params.Slack)
	require.Equal(t, "linux", creator.request.Params.Pool)
	require.Equal(t, "Investigate", creator.request.Params.Message)
	require.Equal(t, "profile", creator.request.SessionProfileID)
	require.Equal(t, "org/repo", creator.request.Tags["repository"])
	require.Equal(t, "triggered_user", creator.request.Params.CredentialSource)
	require.Equal(t, 3, creator.request.Params.CycleMaxCount)
}
