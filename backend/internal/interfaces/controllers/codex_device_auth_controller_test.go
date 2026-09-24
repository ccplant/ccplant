package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
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

// fakeAttemptStore emulates the durable store shared by several API replicas.
// Every read and write copies the record so that in-process mutation cannot
// leak between replicas, mirroring the serialized round trip through the
// backing store.
type fakeAttemptStore struct {
	mu       sync.Mutex
	attempts map[string]*codexauth.Attempt
	locks    map[string]string
}

func newFakeAttemptStore() *fakeAttemptStore {
	return &fakeAttemptStore{attempts: map[string]*codexauth.Attempt{}, locks: map[string]string{}}
}

func (f *fakeAttemptStore) Create(_ context.Context, attempt *codexauth.Attempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.locks[attempt.CredentialName]; ok {
		return codexauth.ErrAttemptActive
	}
	f.locks[attempt.CredentialName] = attempt.ID
	f.attempts[attempt.ID] = cloneAttempt(attempt)
	return nil
}

func (f *fakeAttemptStore) Get(_ context.Context, id string) (*codexauth.Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	attempt, ok := f.attempts[id]
	if !ok {
		return nil, codexauth.ErrAttemptNotFound
	}
	return cloneAttempt(attempt), nil
}

func (f *fakeAttemptStore) Update(_ context.Context, attempt *codexauth.Attempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.attempts[attempt.ID]; !ok {
		return codexauth.ErrAttemptNotFound
	}
	f.attempts[attempt.ID] = cloneAttempt(attempt)
	return nil
}

func (f *fakeAttemptStore) ActiveByCredential(_ context.Context, name string) (*codexauth.Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.locks[name]
	if !ok {
		return nil, codexauth.ErrAttemptNotFound
	}
	attempt, ok := f.attempts[id]
	if !ok {
		return nil, codexauth.ErrAttemptNotFound
	}
	return cloneAttempt(attempt), nil
}

func (f *fakeAttemptStore) LatestByUser(_ context.Context, userID string) (*codexauth.Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *codexauth.Attempt
	for _, attempt := range f.attempts {
		if attempt.UserID != userID {
			continue
		}
		if latest == nil || attempt.ExpiresAt.After(latest.ExpiresAt) {
			latest = attempt
		}
	}
	if latest == nil {
		return nil, codexauth.ErrAttemptNotFound
	}
	return cloneAttempt(latest), nil
}

func (f *fakeAttemptStore) Release(_ context.Context, attempt *codexauth.Attempt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.locks, attempt.CredentialName)
	delete(f.attempts, attempt.ID)
	return nil
}

func cloneAttempt(attempt *codexauth.Attempt) *codexauth.Attempt {
	clone := *attempt
	clone.TokenHash = append([]byte(nil), attempt.TokenHash...)
	return &clone
}

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
	assert.Equal(t, string(sessionrunnercore.SubjectUser), launcher.request.SubjectType)
	assert.Equal(t, "alice", launcher.request.SubjectID)

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

// TestCodexDeviceAuthAttemptStateSharedAcrossReplicas covers the case where the
// replica that receives the auth worker's challenge callback is not the replica
// that answers the UI poll. The durable store must win over the process-local
// cache, otherwise the poll never exposes the user code and the UI stays on
// "waiting" with nothing to enter in the browser.
func TestCodexDeviceAuthAttemptStateSharedAcrossReplicas(t *testing.T) {
	store := newFakeAttemptStore()
	repo := &fakeCodexCredentialsRepository{}
	launcher := &fakeCodexAuthLauncher{}
	owner := NewCodexDeviceAuthController(repo, launcher).WithAttemptStore(store)
	poller := NewCodexDeviceAuthController(repo).WithAttemptStore(store)

	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	startRecorder := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodPost, "/codex/device-auth", strings.NewReader(`{"scope":"user"}`))
	startRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	startRequest.Header.Set("X-Forwarded-Proto", "https")
	startRequest.Host = "proxy.example"
	startContext := e.NewContext(startRequest, startRecorder)
	startContext.Set("internal_user", user)
	require.NoError(t, owner.StartDeviceAuth(startContext))
	require.Equal(t, http.StatusAccepted, startRecorder.Code)
	attemptID := launcher.request.AttemptID

	poll := func(controller *CodexDeviceAuthController) StartDeviceAuthResponse {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/codex/device-auth/"+attemptID, nil)
		ctx := e.NewContext(request, recorder)
		ctx.Set("internal_user", user)
		ctx.SetParamNames("attemptId")
		ctx.SetParamValues(attemptID)
		require.NoError(t, controller.GetAttempt(ctx))
		require.Equal(t, http.StatusOK, recorder.Code)
		var response StartDeviceAuthResponse
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		return response
	}

	require.Equal(t, codexauth.StatusStarting, poll(poller).Status)

	challengeRecorder := httptest.NewRecorder()
	challengeRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"user_code":"ABCD-EFGH","verification_uri":"https://auth.openai.com/device"}`))
	challengeRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+launcher.request.Token)
	challengeContext := e.NewContext(challengeRequest, challengeRecorder)
	challengeContext.SetParamNames("attemptId")
	challengeContext.SetParamValues(attemptID)
	require.NoError(t, owner.ReportChallenge(challengeContext))
	require.Equal(t, http.StatusNoContent, challengeRecorder.Code)

	response := poll(poller)
	require.Equal(t, codexauth.StatusWaitingForUser, response.Status)
	require.Equal(t, "ABCD-EFGH", response.UserCode)
	require.Equal(t, "https://auth.openai.com/device", response.VerificationURI)
}

func TestDeviceAuthCallbackURLUsesForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", nil)
	req.Host = "backend.internal"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "dev.ccplant.com")
	req.Header.Set("X-Forwarded-Prefix", "/api/proxy")

	got, err := requestDerivedCallbackURL(e.NewContext(req, httptest.NewRecorder()))
	require.NoError(t, err)
	assert.Equal(t, "https://dev.ccplant.com/api/proxy/internal/codex-device-auth", got)
}

// TestCodexDeviceAuthCallbackBaseURLOverride covers deployments whose public
// host is only reachable from the browser (for example when the UI sits behind
// Cloudflare Access). The configured base URL must win over the request
// derived host, otherwise the in-cluster auth worker cannot report its
// challenge and the UI stays on "waiting".
func TestCodexDeviceAuthCallbackBaseURLOverride(t *testing.T) {
	repo := &fakeCodexCredentialsRepository{}
	launcher := &fakeCodexAuthLauncher{}
	controller := NewCodexDeviceAuthController(repo, launcher).WithCallbackBaseURL("https://ccplant-api-dev.fly.dev/")
	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", strings.NewReader(`{"scope":"user"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Host = "backend.internal"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "dev.ccplant.com")
	req.Header.Set("X-Forwarded-Prefix", "/api/proxy")
	ctx := e.NewContext(req, recorder)
	ctx.Set("internal_user", user)

	require.NoError(t, controller.StartDeviceAuth(ctx))
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, "https://ccplant-api-dev.fly.dev/internal/codex-device-auth", launcher.request.CallbackURL)
}

func TestCodexDeviceAuthCallbackBaseURLRejectsInvalid(t *testing.T) {
	repo := &fakeCodexCredentialsRepository{}
	launcher := &fakeCodexAuthLauncher{}
	controller := NewCodexDeviceAuthController(repo, launcher).WithCallbackBaseURL("ftp://example.com")
	e := echo.New()
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", strings.NewReader(`{"scope":"user"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := e.NewContext(req, recorder)
	ctx.Set("internal_user", user)

	err := controller.StartDeviceAuth(ctx)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, err.(*echo.HTTPError).Code)
}

func TestDeviceAuthCallbackURLRejectsInvalidForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/codex/device-auth", nil)
	req.Host = "proxy.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/api/../admin")

	_, err := requestDerivedCallbackURL(e.NewContext(req, httptest.NewRecorder()))
	require.Error(t, err)
}
