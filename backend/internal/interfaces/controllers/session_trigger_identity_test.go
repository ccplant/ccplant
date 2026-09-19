package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

type triggerIdentityCreator struct {
	request  entities.StartRequest
	id, user string
	teams    []string
}

type triggerIdentityManager struct{ repositories.SessionManager }

func (triggerIdentityManager) GetSession(string) entities.Session { return nil }

func (m triggerIdentityManager) GetSessionManager() repositories.SessionManager { return m }

func (s *triggerIdentityCreator) CreateSession(_ context.Context, id string, req entities.StartRequest, user, role string, teams []string) (entities.Session, error) {
	s.request, s.id, s.user, s.teams = req, id, user, teams
	return entities.NewProxySessionWithStatus(id, user, req.Scope, req.TeamID, req.Tags, time.Now(), "creating"), nil
}
func (*triggerIdentityCreator) DeleteSessionByID(string) error { return nil }

func TestTriggerStartBindsSignedIdentity(t *testing.T) {
	for _, origin := range []string{"schedule_id", "webhook_id", "slackbot_id"} {
		t.Run(origin, func(t *testing.T) {
			claims := executiontoken.ExecutionClaims{SessionID: "session", UserID: "owner", TriggeredUserID: "actor", Scope: entities.ScopeTeam, TeamID: "org/team", Teams: []string{"org/team"}}
			switch origin {
			case "schedule_id":
				claims.ScheduleID = "schedule"
			case "webhook_id":
				claims.WebhookID = "webhook"
			case "slackbot_id":
				claims.SlackBotID = "bot"
			}
			e := echo.New()
			request := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(`{"scope":"user","team_id":"other/team","tags":{"schedule_id":"forged","webhook_id":"forged","slackbot_id":"forged"},"params":{"oneshot":true}}`))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			recorder := httptest.NewRecorder()
			c := e.NewContext(request, recorder)
			c.Set("trigger_execution_claims", claims)
			c.Set("authz_context", &auth.AuthorizationContext{User: entities.NewUser("owner", entities.UserTypeRegular, "owner"), PersonalScope: auth.PersonalScopeAuth{UserID: "owner", CanCreate: true}, TeamScope: auth.TeamScopeAuth{Teams: claims.Teams, TeamPermissions: map[string]auth.TeamPermissions{"org/team": {TeamID: "org/team", CanCreate: true}}}})
			creator := &triggerIdentityCreator{}
			controller := NewSessionController(triggerIdentityManager{}, creator)
			require.NoError(t, controller.StartSession(c))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, claims.SessionID, creator.id)
			require.Equal(t, claims.UserID, creator.user)
			require.Equal(t, claims.TriggeredUserID, creator.request.TriggeredUserID)
			require.Equal(t, claims.Scope, creator.request.Scope)
			require.Equal(t, claims.TeamID, creator.request.TeamID)
			require.Equal(t, claims.Teams, creator.teams)
			require.True(t, creator.request.Params.Oneshot)
			for key, id := range map[string]string{"schedule_id": claims.ScheduleID, "webhook_id": claims.WebhookID, "slackbot_id": claims.SlackBotID} {
				require.Equal(t, id, creator.request.Tags[key])
			}
			var response map[string]string
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, "session", response["session_id"])
		})
	}
}
