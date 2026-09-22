package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type githubConnectionURLResolverStub struct {
	baseURL string
	apiURL  string
}

func (s githubConnectionURLResolverStub) ResolveAccessToken(context.Context, *entities.User, string) (string, error) {
	return "", nil
}

func (s githubConnectionURLResolverStub) ResolveAccessTokenForOrganization(context.Context, *entities.User, string) (string, string, bool, error) {
	return "", "", false, nil
}

func (s githubConnectionURLResolverStub) IssueBrokerLeaseForOrganization(context.Context, string, string, string) (string, string, bool, error) {
	return "", "", false, nil
}

func (s githubConnectionURLResolverStub) ResolveConnectionURLs(context.Context, string) (string, string, error) {
	return s.baseURL, s.apiURL, nil
}

func (s githubConnectionURLResolverStub) RevokeBrokerLeases(context.Context, string) error {
	return nil
}

func TestSessionTokenDebugLogging(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	controller := NewSessionController(nil, nil)
	controller.logSessionTokenRouting("session-disabled", "explicit", "connection-a", "secret-token-a")
	if output.Len() != 0 {
		t.Fatalf("disabled debug logging produced output: %q", output.String())
	}

	controller.sessionTokenDebug = true
	controller.logSessionTokenRouting("session-enabled", "organization", "connection-b", "secret-token-b")
	got := output.String()
	if !strings.Contains(got, "session_id=session-enabled") || !strings.Contains(got, `connection_id="connection-b"`) || !strings.Contains(got, "token_fingerprint=35ef9b2c10f6") {
		t.Fatalf("unexpected debug output: %q", got)
	}
	if strings.Contains(got, "secret-token-b") {
		t.Fatalf("debug output leaked token: %q", got)
	}
}

type quotaErrorSessionCreator struct{}

func (quotaErrorSessionCreator) CreateSession(context.Context, string, entities.StartRequest, string, string, []string) (entities.Session, error) {
	return nil, &sessionrunnercore.QuotaExceededError{
		Pool: "linux", BindingID: "binding-alice", MaxConcurrent: 2, Active: 2,
	}
}

func (quotaErrorSessionCreator) DeleteSessionByID(string) error { return nil }

type previewSessionCreator struct {
	createCalled bool
}

func (p *previewSessionCreator) CreateSession(context.Context, string, entities.StartRequest, string, string, []string) (entities.Session, error) {
	p.createCalled = true
	return nil, errors.New("CreateSession must not be called by dry-run")
}

func (*previewSessionCreator) DeleteSessionByID(string) error { return nil }

func (*previewSessionCreator) PreviewSession(_ context.Context, _ string, req entities.StartRequest, _ string, _ string, _ []string) (*entities.SessionStartPreview, error) {
	return &entities.SessionStartPreview{
		Placement: entities.SessionStartPlacement{Transport: repositories.SessionRouteTransportDirectRuntime, Pool: "linux", BindingID: "binding-alice"},
		Settings: &sessionsettings.SessionSettings{
			Session:     sessionsettings.SessionMeta{UserID: "alice", Scope: string(req.Scope)},
			Env:         map[string]string{"VISIBLE_NAME": "secret-value"},
			Credentials: "managed-secret",
		},
	}, nil
}

func TestStartSessionDryRunReturnsRedactedPlanWithoutCreatingSession(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user","environment":{"CUSTOM":"sensitive"},"params":{"github_token":"ghp_secret"}}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser(entities.UserID("alice"), entities.UserTypeAPIKey, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanCreate: true, CanRead: true},
	})
	creator := &previewSessionCreator{}
	controller := NewSessionController(nil, creator)

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, creator.createCalled)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, true, body["dry_run"])
	require.Equal(t, "create", body["decision"])
	require.Equal(t, "linux", body["placement"].(map[string]interface{})["pool"])
	effective := body["effective_request"].(map[string]interface{})
	require.Equal(t, "<redacted>", effective["environment"].(map[string]interface{})["CUSTOM"])
	require.Equal(t, "<redacted>", effective["params"].(map[string]interface{})["github_token"])
	settings := body["settings"].(map[string]interface{})
	require.Equal(t, "<redacted>", settings["env"].(map[string]interface{})["VISIBLE_NAME"])
	require.Equal(t, "<redacted>", settings["credentials"])
}

func TestStartSessionRejectsInvalidDryRun(t *testing.T) {
	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest(http.MethodPost, "/start?dry_run=maybe", nil), httptest.NewRecorder())
	err := NewSessionController(nil, nil).StartSession(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestStartSessionReturnsQuotaExceeded(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(`{"scope":"user"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeAPIKey, "alice")
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User: user,
		PersonalScope: auth.PersonalScopeAuth{
			UserID: "alice", CanCreate: true, CanRead: true,
		},
	})

	controller := NewSessionController(nil, quotaErrorSessionCreator{})
	if err := controller.StartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["binding_id"] != "binding-alice" || body["max_concurrent"] != float64(2) || body["active"] != float64(2) {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestPopulateGitHubTokenFromAuthHeader(t *testing.T) {
	tests := []struct {
		name          string
		scope         entities.ResourceScope
		existingToken string
		credential    *auth.CredentialContext
		wantToken     string
	}{
		{name: "user scope receives authenticated GitHub token", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: "oauth-token"},
		{name: "API key is never treated as GitHub token", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindAPIKey, Token: "api-key"}, wantToken: ""},
		{name: "explicit token is preserved", scope: entities.ScopeUser, existingToken: "explicit-token", credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: "explicit-token"},
		{name: "team scope excludes user token", scope: entities.ScopeTeam, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/start", nil)
			req.Header.Set("Authorization", "Bearer oauth-token")
			ctx := e.NewContext(req, httptest.NewRecorder())
			auth.SetCredentialContext(ctx, tt.credential)
			startReq := entities.StartRequest{Scope: tt.scope, Params: &entities.SessionParams{GithubToken: tt.existingToken}}

			populateGitHubTokenFromAuthHeader(ctx, &startReq)

			if startReq.Params.GithubToken != tt.wantToken {
				t.Fatalf("GithubToken = %q, want %q", startReq.Params.GithubToken, tt.wantToken)
			}
		})
	}
}

func TestRepositoryOwner(t *testing.T) {
	t.Parallel()
	if got := repositoryOwner(" Example-Org/repository "); got != "example-org" {
		t.Fatalf("repositoryOwner() = %q", got)
	}
	for _, value := range []string{"", "repository", "/repository", "org/"} {
		if got := repositoryOwner(value); got != "" {
			t.Fatalf("repositoryOwner(%q) = %q, want empty", value, got)
		}
	}
}

func TestApplyGitHubConnectionURLsOverridesDeploymentDefaults(t *testing.T) {
	controller := &SessionController{githubTokenResolver: githubConnectionURLResolverStub{
		baseURL: "https://github.selected.example",
		apiURL:  "https://github.selected.example/api/v3",
	}}
	startReq := entities.StartRequest{Environment: map[string]string{
		"GITHUB_URL": "https://github.enterprise.example",
		"GITHUB_API": "https://github.enterprise.example/api/v3",
		"GH_HOST":    "github.enterprise.example",
	}}

	require.NoError(t, controller.applyGitHubConnectionURLs(context.Background(), &startReq, "selected"))
	require.Equal(t, "https://github.selected.example", startReq.Environment["GITHUB_URL"])
	require.Equal(t, "https://github.selected.example/api/v3", startReq.Environment["GITHUB_API"])
	require.Equal(t, "github.selected.example", startReq.Environment["GH_HOST"])
}

func TestApplyGitHubConnectionURLsClearsEnterpriseHostForGitHubDotCom(t *testing.T) {
	controller := &SessionController{githubTokenResolver: githubConnectionURLResolverStub{
		baseURL: "https://github.com",
		apiURL:  "https://api.github.com",
	}}
	startReq := entities.StartRequest{Environment: map[string]string{
		"GITHUB_URL": "https://github.enterprise.example",
		"GITHUB_API": "https://github.enterprise.example/api/v3",
		"GH_HOST":    "github.enterprise.example",
	}}

	require.NoError(t, controller.applyGitHubConnectionURLs(context.Background(), &startReq, "selected"))
	require.Equal(t, "https://github.com", startReq.Environment["GITHUB_URL"])
	require.Equal(t, "https://api.github.com", startReq.Environment["GITHUB_API"])
	require.Equal(t, "github.com", startReq.Environment["GH_HOST"])
}

func TestSessionRepository(t *testing.T) {
	tests := []struct {
		name  string
		input entities.StartRequest
		want  string
	}{
		{name: "params takes precedence", input: entities.StartRequest{Params: &entities.SessionParams{RepoFullName: "params/repo"}, Tags: map[string]string{"repository": "tags/repo"}}, want: "params/repo"},
		{name: "falls back to repository tag", input: entities.StartRequest{Params: &entities.SessionParams{}, Tags: map[string]string{"repository": "tags/repo"}}, want: "tags/repo"},
		{name: "supports nil params", input: entities.StartRequest{Tags: map[string]string{"repository": "tags/repo"}}, want: "tags/repo"},
		{name: "empty when unspecified", input: entities.StartRequest{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionRepository(tt.input); got != tt.want {
				t.Fatalf("sessionRepository() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShouldUseGitHubBrokerPreservesExplicitCredentials(t *testing.T) {
	tests := []struct {
		name  string
		input entities.StartRequest
		repo  string
		want  bool
	}{
		{name: "team repository uses broker", input: entities.StartRequest{Scope: entities.ScopeTeam, Params: &entities.SessionParams{}}, repo: "acme/repo", want: true},
		{name: "legacy explicit token wins", input: entities.StartRequest{Scope: entities.ScopeTeam, Params: &entities.SessionParams{GithubToken: "legacy-token"}}, repo: "acme/repo", want: false},
		{name: "explicit connection wins", input: entities.StartRequest{Scope: entities.ScopeTeam, Params: &entities.SessionParams{ConnectionID: "connection-1"}}, repo: "acme/repo", want: false},
		{name: "user session does not use broker", input: entities.StartRequest{Scope: entities.ScopeUser, Params: &entities.SessionParams{}}, repo: "acme/repo", want: false},
		{name: "repository is required", input: entities.StartRequest{Scope: entities.ScopeTeam, Params: &entities.SessionParams{}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseGitHubBroker(tt.input, tt.repo); got != tt.want {
				t.Fatalf("shouldUseGitHubBroker() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestGitHubBrokerURLUsesForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Host = "backend.internal"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "dev.ccplant.com")
	req.Header.Set("X-Forwarded-Prefix", "/api/proxy")

	got, err := githubBrokerURL(e.NewContext(req, httptest.NewRecorder()), "session/id", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://dev.ccplant.com/api/proxy/internal/sessions/session%2Fid/github-credentials"
	if got != want {
		t.Fatalf("githubBrokerURL() = %q, want %q", got, want)
	}
}

func TestGitHubBrokerURLRejectsInvalidForwardedPrefix(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start", nil)
	req.Host = "backend.internal"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/api/../admin")

	if _, err := githubBrokerURL(e.NewContext(req, httptest.NewRecorder()), "session-1", ""); err == nil {
		t.Fatal("githubBrokerURL() accepted an invalid forwarded prefix")
	}
}

func TestGitHubBrokerURLConfiguredBase(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "internal service", base: "http://backend.internal:8080", want: "http://backend.internal:8080"},
		{name: "public API prefix", base: "https://broker.example.test/api/proxy/", want: "https://broker.example.test/api/proxy"},
		{name: "trailing slashes", base: "https://broker.example.test///", want: "https://broker.example.test"},
		{name: "surrounding whitespace", base: " https://broker.example.test/api/v1/ \n", want: "https://broker.example.test/api/v1"},
		{name: "escaped prefix", base: "https://broker.example.test/team%20api", want: "https://broker.example.test/team%20api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "https://ui.example.test/start", nil)
			req.Header.Set("X-Forwarded-Host", "ui.example.test")
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Prefix", "/browser-api")
			controller := NewSessionController(nil, nil, WithGitHubBrokerBaseURL(tt.base))
			got, err := githubBrokerURL(e.NewContext(req, httptest.NewRecorder()), "session/id", controller.githubBrokerBaseURL)
			require.NoError(t, err)
			require.Equal(t, tt.want+"/internal/sessions/session%2Fid/github-credentials", got)
		})
	}
}

func TestGitHubBrokerURLRejectsInvalidConfiguredBase(t *testing.T) {
	for _, base := range []string{
		"/api/proxy", "//broker.example.test", "ftp://broker.example.test", "https:///api",
		"https://user:secret@broker.example.test", "https://broker.example.test?token=secret",
		"https://broker.example.test#fragment", "https://broker.example.test?", "https://broker.example.test#",
		"https://broker.example.test/%zz", "https://broker.example.test\r\nInjected: value",
	} {
		t.Run(base, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://ui.example.test/start", nil)
			got, err := githubBrokerURL(echo.New().NewContext(req, httptest.NewRecorder()), "session-1", base)
			require.ErrorContains(t, err, "github_broker_base_url")
			require.Empty(t, got, "invalid configuration must not fall back to request headers")
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

type sessionListTestSession struct {
	id     string
	status string
}

func (s *sessionListTestSession) ID() string                    { return s.id }
func (s *sessionListTestSession) Addr() string                  { return "" }
func (s *sessionListTestSession) UserID() string                { return "user-1" }
func (s *sessionListTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *sessionListTestSession) TeamID() string                { return "" }
func (s *sessionListTestSession) Tags() map[string]string       { return nil }
func (s *sessionListTestSession) Status() string {
	if s.status == "" {
		return "running"
	}
	return s.status
}
func (s *sessionListTestSession) StartedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) UpdatedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) LastMessageAt() time.Time { return time.Time{} }
func (s *sessionListTestSession) Description() string      { return "" }
func (s *sessionListTestSession) Cancel()                  {}

func TestExcludeAllocatedSessions(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "public-id"},
		&sessionListTestSession{id: "allocated-id"},
		&sessionListTestSession{id: "local-id"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-id", RemoteSessionID: "allocated-id"},
	}

	got := excludeAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("excludeAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got[0].ID() != "public-id" || got[1].ID() != "local-id" {
		t.Fatalf("excludeAllocatedSessions() returned IDs %q and %q, want public-id and local-id", got[0].ID(), got[1].ID())
	}
}

func TestIndexAllocatedSessionsPreservesRuntimeStatus(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "allocated-running", status: "running"},
		&sessionListTestSession{id: "allocated-stable", status: "stable"},
		&sessionListTestSession{id: "local-id", status: "active"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-running", RemoteSessionID: "allocated-running"},
		{SessionID: "public-stable", RemoteSessionID: "allocated-stable"},
	}

	got := indexAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("indexAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got["allocated-running"].Status() != "running" {
		t.Fatalf("running session status = %q, want running", got["allocated-running"].Status())
	}
	if got["allocated-stable"].Status() != "stable" {
		t.Fatalf("stable session status = %q, want stable", got["allocated-stable"].Status())
	}
	if status := routedSessionStatus(routes[0], got); status != "running" {
		t.Fatalf("public running session status = %q, want running", status)
	}
	if status := routedSessionStatus(routes[1], got); status != "stable" {
		t.Fatalf("public stable session status = %q, want stable", status)
	}
}

func TestRoutedSessionStatusFallbacks(t *testing.T) {
	tests := []struct {
		name  string
		route *repositories.SessionRoute
		want  string
	}{
		{name: "allocation pending", route: &repositories.SessionRoute{SessionID: "public-id"}, want: "creating"},
		{name: "assigned but unconfirmed session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id"}, want: "starting"},
		{name: "confirmed session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id", Status: "stable"}, want: "stable"},
		{name: "direct runtime pushed status", route: &repositories.SessionRoute{SessionID: "public-id", Transport: repositories.SessionRouteTransportDirectRuntime, Status: "active"}, want: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedSessionStatus(tt.route, nil); got != tt.want {
				t.Fatalf("routedSessionStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindUncreatedSessionAllocation(t *testing.T) {
	pending := &sessionListTestSession{id: "pending-id", status: "pending"}
	sessions := []entities.Session{
		&sessionListTestSession{id: "running-id", status: "running"},
		pending,
		&sessionListTestSession{id: "allocating-id", status: "allocating"},
	}

	if got := findUncreatedSessionAllocation(sessions, "pending-id"); got != pending {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want pending session", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "running-id"); got != nil {
		t.Fatalf("findUncreatedSessionAllocation() returned running session %v", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "allocating-id"); got != sessions[2] {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want allocating session", got)
	}
}
