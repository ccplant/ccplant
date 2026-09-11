package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

type fakeCodexAuthLauncher struct{ request codexauth.WorkloadRequest }

func (f *fakeCodexAuthLauncher) StartCodexDeviceAuth(_ context.Context, request codexauth.WorkloadRequest) error {
	f.request = request
	return nil
}
func (f *fakeCodexAuthLauncher) CancelCodexDeviceAuth(context.Context, string) error { return nil }

type fakeCodexCredentialsRepository struct{ saved *entities.Credentials }

func (f *fakeCodexCredentialsRepository) Save(_ context.Context, value *entities.Credentials) error {
	f.saved = value
	return nil
}
func (f *fakeCodexCredentialsRepository) FindByName(context.Context, string) (*entities.Credentials, error) {
	return nil, nil
}
func (f *fakeCodexCredentialsRepository) Delete(context.Context, string) error { return nil }
func (f *fakeCodexCredentialsRepository) Exists(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fakeCodexCredentialsRepository) List(context.Context) ([]*entities.Credentials, error) {
	return nil, nil
}

func TestDeviceAuthCredentialName(t *testing.T) {
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)
	user.SetGitHubInfo(entities.NewGitHubUserInfo(1, "alice", "Alice", "alice@example.com", "", "", ""), []entities.GitHubTeamMembership{
		{Organization: "acme", TeamSlug: "platform"},
	})

	tests := []struct {
		name       string
		req        StartDeviceAuthRequest
		want       string
		wantStatus int
	}{
		{name: "default user scope", want: "alice"},
		{name: "explicit user scope", req: StartDeviceAuthRequest{Scope: "user"}, want: "alice"},
		{name: "team scope", req: StartDeviceAuthRequest{Scope: "team", TeamID: "acme/platform"}, want: "acme/platform"},
		{name: "team id with user scope", req: StartDeviceAuthRequest{Scope: "user", TeamID: "acme/platform"}, wantStatus: http.StatusBadRequest},
		{name: "missing team id", req: StartDeviceAuthRequest{Scope: "team"}, wantStatus: http.StatusBadRequest},
		{name: "unknown team", req: StartDeviceAuthRequest{Scope: "team", TeamID: "acme/security"}, wantStatus: http.StatusForbidden},
		{name: "invalid scope", req: StartDeviceAuthRequest{Scope: "organization"}, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deviceAuthCredentialName(user, tt.req)
			if tt.wantStatus != 0 {
				require.Error(t, err)
				var httpErr *echo.HTTPError
				require.True(t, errors.As(err, &httpErr))
				assert.Equal(t, tt.wantStatus, httpErr.Code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCodexDeviceAuthWorkloadFlow(t *testing.T) {
	repo := &fakeCodexCredentialsRepository{}
	launcher := &fakeCodexAuthLauncher{}
	controller := NewCodexDeviceAuthController(repo, launcher)
	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/codex/device-auth", strings.NewReader(`{"scope":"user"}`))
	startRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	startRequest.Header.Set("X-Forwarded-Proto", "https")
	startRequest.Host = "proxy.example"
	startContext := e.NewContext(startRequest, startRecorder)
	startContext.Set("internal_user", user)
	require.NoError(t, controller.StartDeviceAuth(startContext))
	assert.Equal(t, http.StatusAccepted, startRecorder.Code)
	require.Regexp(t, `^cda-[0-9a-f]{32}$`, launcher.request.AttemptID)
	assert.Equal(t, "https://proxy.example/internal/codex-device-auth", launcher.request.CallbackURL)

	challengeRecorder := httptest.NewRecorder()
	challengeRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"user_code":"ABCD-EFGH","verification_uri":"https://auth.openai.com/device"}`))
	challengeRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	challengeContext := e.NewContext(challengeRequest, challengeRecorder)
	challengeContext.SetParamNames("attemptId")
	challengeContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.ReportChallenge(challengeContext))
	assert.Equal(t, http.StatusNoContent, challengeRecorder.Code)

	resultBody, _ := json.Marshal(codexauth.Result{Status: codexauth.StatusAuthorized, AuthJSON: []byte(`{"tokens":{"access_token":"secret"}}`)})
	resultRecorder := httptest.NewRecorder()
	resultRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(resultBody)))
	resultRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	resultContext := e.NewContext(resultRequest, resultRecorder)
	resultContext.SetParamNames("attemptId")
	resultContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.ReportResult(resultContext))
	assert.Equal(t, http.StatusNoContent, resultRecorder.Code)
	require.NotNil(t, repo.saved)
	assert.Equal(t, "alice", repo.saved.Name())
}

func TestDeviceAuthCallbackURLUsesForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", nil)
	req.Host = "backend.internal"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "dev.ccplant.com")
	req.Header.Set("X-Forwarded-Prefix", "/api/v1")

	got, err := deviceAuthCallbackURL(e.NewContext(req, httptest.NewRecorder()))
	require.NoError(t, err)
	assert.Equal(t, "https://dev.ccplant.com/api/v1/internal/codex-device-auth", got)
}

func TestDeviceAuthCallbackURLRejectsInvalidForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", nil)
	req.Host = "proxy.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/api/../admin")

	_, err := deviceAuthCallbackURL(e.NewContext(req, httptest.NewRecorder()))
	require.Error(t, err)
}
