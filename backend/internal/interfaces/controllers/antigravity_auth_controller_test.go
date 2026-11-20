package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestAntigravityBusinessSSOWorkloadFlow(t *testing.T) {
	repo := &fakeCodexCredentialsRepository{}
	launcher := &fakeCodexAuthLauncher{}
	controller := NewAntigravityAuthController(repo, launcher)
	controller.WithCallbackBaseURL("https://callback.invalid")
	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/antigravity/auth/sso", strings.NewReader(`{"scope":"user"}`))
	startRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	startContext := e.NewContext(startRequest, startRecorder)
	startContext.Set("internal_user", user)
	require.NoError(t, controller.StartSSO(startContext))
	require.Equal(t, http.StatusAccepted, startRecorder.Code)
	require.Equal(t, "antigravity", launcher.request.Provider)
	require.Equal(t, "https://callback.invalid/internal/antigravity-auth", launcher.request.CallbackURL)

	challenge := `{"verification_uri":"https://accounts.google.com/o/oauth2/auth","transcript":"Open the URL and enter the code"}`
	challengeRecorder := httptest.NewRecorder()
	challengeRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(challenge))
	challengeRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	challengeContext := e.NewContext(challengeRequest, challengeRecorder)
	challengeContext.SetParamNames("attemptId")
	challengeContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.ReportChallenge(challengeContext))

	bundle := json.RawMessage(`{"version":1,"files":[{"path":"oauth.json","content_base64":"e30="}]}`)
	resultBody, err := json.Marshal(codexauth.Result{Status: codexauth.StatusAuthorized, AuthJSON: bundle})
	require.NoError(t, err)
	resultRecorder := httptest.NewRecorder()
	resultRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(resultBody)))
	resultRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	resultContext := e.NewContext(resultRequest, resultRecorder)
	resultContext.SetParamNames("attemptId")
	resultContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.ReportResult(resultContext))
	require.Equal(t, http.StatusNoContent, resultRecorder.Code)
	require.NotNil(t, repo.saved)
	require.Equal(t, sessionsettings.FileTypeAntigravityAuth, repo.saved.FileType())
}

func TestAntigravityFailedWorkloadExposesDiagnosticAndIsRetained(t *testing.T) {
	launcher := &fakeCodexAuthLauncher{}
	controller := NewAntigravityAuthController(&fakeCodexCredentialsRepository{}, launcher)
	controller.WithCallbackBaseURL("https://callback.invalid")
	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/antigravity/auth/sso", strings.NewReader(`{"scope":"user"}`))
	startRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	startContext := e.NewContext(startRequest, startRecorder)
	startContext.Set("internal_user", user)
	require.NoError(t, controller.StartSSO(startContext))

	resultBody, err := json.Marshal(codexauth.Result{Status: codexauth.StatusFailed, ErrorCode: "antigravity_login_failed", ErrorMessage: "agy executable was not found"})
	require.NoError(t, err)
	resultRecorder := httptest.NewRecorder()
	resultRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(resultBody)))
	resultRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	resultContext := e.NewContext(resultRequest, resultRecorder)
	resultContext.SetParamNames("attemptId")
	resultContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.ReportResult(resultContext))
	require.Equal(t, 0, launcher.cancelCount, "failed workload must remain available for diagnostics")

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	getContext := e.NewContext(getRequest, getRecorder)
	getContext.Set("internal_user", user)
	getContext.SetParamNames("attemptId")
	getContext.SetParamValues(launcher.request.AttemptID)
	require.NoError(t, controller.GetAttempt(getContext))
	var response AntigravityAuthResponse
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &response))
	require.Equal(t, codexauth.StatusFailed, response.Status)
	require.Equal(t, "antigravity_login_failed", response.ErrorCode)
	require.Equal(t, "agy executable was not found", response.ErrorMessage)
}

func TestValidAntigravityInput(t *testing.T) {
	require.True(t, validAntigravityInput(antigravityAuthInputRequest{Input: "authorization-code"}))
	first, last, negative, tooLarge := 0, 100, -1, 101
	require.True(t, validAntigravityInput(antigravityAuthInputRequest{SubscriptionIndex: &first}))
	require.True(t, validAntigravityInput(antigravityAuthInputRequest{SubscriptionIndex: &last}))
	require.False(t, validAntigravityInput(antigravityAuthInputRequest{}))
	require.False(t, validAntigravityInput(antigravityAuthInputRequest{Input: "code", SubscriptionIndex: &first}))
	require.False(t, validAntigravityInput(antigravityAuthInputRequest{SubscriptionIndex: &negative}))
	require.False(t, validAntigravityInput(antigravityAuthInputRequest{SubscriptionIndex: &tooLarge}))
}
