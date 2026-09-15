package controlapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

func TestTriggerStartPreservesConfigurationAndIdentity(t *testing.T) {
	for _, origin := range []string{"schedule_id", "webhook_id", "slackbot_id"} {
		t.Run(origin, func(t *testing.T) {
			var start entities.StartRequest
			var claims executiontoken.ExecutionClaims
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/start" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var err error
				claims, err = executiontoken.VerifyExecutionToken([]byte("secret"), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Now())
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if err = json.NewDecoder(r.Body).Decode(&start); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Header.Get("Idempotency-Key") != "execution" {
					t.Error("missing execution id")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "execution"})
			}))
			defer api.Close()
			control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("creation reached control API: %s", r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer control.Close()
			wait := 3
			authProxy := false
			req := &entities.RunServerRequest{
				UserID: "owner", TriggeredUserID: "actor", Scope: entities.ScopeTeam, TeamID: "org/team", Teams: []string{"org/team"},
				Tags: map[string]string{origin: "trigger", "branch": "main", "pr": "42"}, Pool: "pool", AgentType: "codex-acp", Model: "model",
				InitialMessage: "finish", Oneshot: true, SessionTTL: "2m", InitialMessageWaitSecond: &wait,
				Environment: map[string]string{"EXPLICIT": "value"}, MemoryKey: map[string]string{"task": "test"}, ResolvedSessionProfileID: "profile",
				RepoInfo: &entities.RepositoryInfo{FullName: "org/repo"}, CycleMessage: "continue", CycleMaxCount: 2,
				Docker: &entities.DockerParams{Enabled: true}, Sandbox: &entities.SandboxParams{Enabled: true}, AuthProxy: &authProxy,
				CredentialSource: "triggered_user", CodexAuthMode: "oauth", ClaudeAuthMode: "api_key", UnsyncedFilePaths: []string{"/tmp/private"},
			}
			payload := []byte(`{"event":"test"}`)
			session, err := NewSessionManager(control.URL, "secret").WithSessionAPIURL(api.URL).CreateSession(context.Background(), "execution", req, payload)
			require.NoError(t, err)
			require.Equal(t, "execution", session.ID())
			require.Equal(t, "owner", claims.UserID)
			require.Equal(t, "actor", claims.TriggeredUserID)
			require.Equal(t, req.Teams, claims.Teams)
			require.Equal(t, req.Scope, claims.Scope)
			require.Equal(t, req.TeamID, claims.TeamID)
			require.Equal(t, "execution", claims.SessionID)
			require.Equal(t, "trigger", map[string]string{"schedule_id": claims.ScheduleID, "webhook_id": claims.WebhookID, "slackbot_id": claims.SlackBotID}[origin])
			require.Equal(t, req.Environment, start.Environment)
			require.Equal(t, req.MemoryKey, start.MemoryKey)
			require.Equal(t, "profile", start.SessionProfileID)
			require.Equal(t, payload, start.WebhookPayload)
			require.Equal(t, "org/repo", start.Tags["repository"])
			require.NotContains(t, req.Tags, "repository", "caller tags must not be mutated")
			require.Equal(t, "main", start.Tags["branch"])
			require.Equal(t, "42", start.Tags["pr"])
			require.Equal(t, &entities.SessionParams{Pool: "pool", RepoFullName: "org/repo", Message: "finish", AgentType: "codex-acp", Model: "model", Oneshot: true, SessionTTL: "2m", InitialMessageWaitSecond: &wait, CycleMessage: "continue", CycleMaxCount: 2, Docker: req.Docker, Sandbox: req.Sandbox, AuthProxy: &authProxy, CredentialSource: "triggered_user", CodexAuthMode: "oauth", ClaudeAuthMode: "api_key", UnsyncedFilePaths: req.UnsyncedFilePaths}, start.Params)
		})
	}
}

func TestTriggerStartFailureDoesNotFallBack(t *testing.T) {
	for _, origin := range []string{"schedule_id", "webhook_id"} {
		t.Run(origin, func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/start" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer api.Close()
			_, err := NewSessionManager(api.URL, "secret").CreateSession(context.Background(), "session", &entities.RunServerRequest{UserID: "owner", Tags: map[string]string{origin: "trigger"}}, nil)
			require.ErrorContains(t, err, "503")
			require.Equal(t, 1, calls)
			_, err = NewSessionManager(api.URL, "").CreateSession(context.Background(), "session", &entities.RunServerRequest{UserID: "owner", Tags: map[string]string{origin: "trigger"}}, nil)
			require.ErrorContains(t, err, "signing key is required")
			require.Equal(t, 1, calls)
		})
	}
}
