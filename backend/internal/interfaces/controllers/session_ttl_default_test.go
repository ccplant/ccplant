package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

func TestStartAppliesDefaultSessionTTL(t *testing.T) {
	schedule := &executiontoken.ExecutionClaims{ScheduleID: "schedule"}
	webhook := &executiontoken.ExecutionClaims{WebhookID: "webhook"}
	slackbot := &executiontoken.ExecutionClaims{SlackBotID: "bot"}
	all := []SessionControllerOption{WithScheduleSessionTTL("24h"), WithWebhookSessionTTL("48h"), WithInteractiveSessionTTL("168h")}
	cases := []struct {
		name    string
		claims  *executiontoken.ExecutionClaims
		options []SessionControllerOption
		body    string
		want    string
	}{
		{name: "schedule without ttl", claims: schedule, options: all, body: `{}`, want: "24h"},
		{name: "webhook without ttl", claims: webhook, options: all, body: `{}`, want: "48h"},
		{name: "schedule with longer ttl", claims: schedule, options: all, body: `{"params":{"session_ttl":"72h"}}`, want: "72h"},
		{name: "webhook with shorter ttl", claims: webhook, options: all, body: `{"params":{"session_ttl":"30m"}}`, want: "30m"},
		{name: "schedule oneshot", claims: schedule, options: all, body: `{"params":{"oneshot":true}}`, want: "1m"},
		{name: "schedule without schedule default", claims: schedule, options: []SessionControllerOption{WithWebhookSessionTTL("48h"), WithInteractiveSessionTTL("168h")}, body: `{}`, want: ""},
		{name: "webhook without webhook default", claims: webhook, options: []SessionControllerOption{WithScheduleSessionTTL("24h"), WithInteractiveSessionTTL("168h")}, body: `{}`, want: ""},
		{name: "slackbot", claims: slackbot, options: all, body: `{}`, want: ""},
		{name: "interactive without ttl", options: all, body: `{}`, want: "168h"},
		{name: "interactive with own ttl", options: all, body: `{"params":{"session_ttl":"2h"}}`, want: "2h"},
		{name: "interactive with trigger tag", options: all, body: `{"tags":{"schedule_id":"forged"}}`, want: "168h"},
		{name: "interactive without interactive default", options: []SessionControllerOption{WithScheduleSessionTTL("24h"), WithWebhookSessionTTL("48h")}, body: `{}`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(tc.body))
			request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			recorder := httptest.NewRecorder()
			c := echo.New().NewContext(request, recorder)
			if tc.claims != nil {
				claims := *tc.claims
				claims.SessionID, claims.UserID, claims.Scope = "session", "owner", entities.ScopeUser
				c.Set("trigger_execution_claims", claims)
			}
			c.Set("authz_context", &auth.AuthorizationContext{User: entities.NewUser("owner", entities.UserTypeRegular, "owner"), PersonalScope: auth.PersonalScopeAuth{UserID: "owner", CanCreate: true}})
			creator := &triggerIdentityCreator{}
			controller := NewSessionController(triggerIdentityManager{}, creator, tc.options...)
			require.NoError(t, controller.StartSession(c))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, tc.want, sessionuc.ResolveSessionTTL(creator.request.Params))
		})
	}
}

func TestApplySessionTTLDefault(t *testing.T) {
	reuse := entities.StartRequest{}
	applySessionTTLDefault(&reuse, "24h", true)
	require.Nil(t, reuse.Params)

	fresh := entities.StartRequest{}
	applySessionTTLDefault(&fresh, "24h", false)
	require.Equal(t, "24h", fresh.Params.SessionTTL)

	fromProfile := entities.StartRequest{Params: &entities.SessionParams{SessionTTL: "48h"}}
	applySessionTTLDefault(&fromProfile, "24h", false)
	require.Equal(t, "48h", fromProfile.Params.SessionTTL)
}
