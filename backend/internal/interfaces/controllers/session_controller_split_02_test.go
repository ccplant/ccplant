package controllers

import (
	"context"
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
)

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

func TestGitHubRoutingDefaultsUsesProfileRepositoryAndConnection(t *testing.T) {
	profile := entities.NewSessionProfile("profile", "Profile", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{ConnectionID: "connection-1"})
	cfg.SetProfileRepositories([]entities.ProfileRepository{{Repository: "acme/shared", Path: "/home/agentapi/workdir/shared"}})
	profile.SetConfig(cfg)
	controller := &SessionController{sessionProfileRepo: githubRoutingProfileRepo{profile: profile}}
	authz := &auth.AuthorizationContext{User: entities.NewUser("user-1", entities.UserTypeRegular, "user-1"), PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true, CanCreate: true}}

	repository, connectionID, err := controller.githubRoutingDefaults(context.Background(), entities.StartRequest{SessionProfileID: "profile", Scope: entities.ScopeUser}, "user-1", authz)
	require.NoError(t, err)
	require.Equal(t, "acme/shared", repository)
	require.Equal(t, "connection-1", connectionID)
}

func TestGitHubRepositoriesForSessionAllowsTeamWithoutSettings(t *testing.T) {
	controller := &SessionController{settingsRepo: missingSettingsRepo{}}
	repositories, err := controller.githubRepositoriesForSession(context.Background(), entities.StartRequest{
		Scope:                     entities.ScopeTeam,
		TeamID:                    "team-1",
		ProfileGitHubRepositories: []string{"^acme/shared$"},
	}, "user-1", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"^acme/shared$"}, repositories)
}

func TestStartPersonalSessionWithoutRepositoryUsesConnectionBroker(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user","params":{"connection_id":"connection-1"}}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser("alice", entities.UserTypeRegular, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanRead: true, CanCreate: true},
	})
	settings := entities.NewSettings("alice")
	settings.SetGitHubRepositories([]string{"^acme/shared$"})
	creator := &previewSessionCreator{}
	controller := NewSessionController(nil, creator, WithGitHubTokenResolver(githubConnectionURLResolverStub{
		baseURL: "https://github.com", apiURL: "https://api.github.com",
	}))
	controller.settingsRepo = githubSettingsRepo{settings: settings}

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, entities.ScopeUser, creator.request.Scope)
	require.Empty(t, sessionRepository(creator.request))
	require.Equal(t, "connection-1", creator.request.Params.ConnectionID)
	require.NotEmpty(t, creator.request.Environment["AGENTAPI_GITHUB_BROKER_URL"])
	require.Equal(t, "<generated-at-apply>", creator.request.Environment["AGENTAPI_GITHUB_BROKER_TOKEN"])
	require.Equal(t, "connection-1", creator.request.Environment["AGENTAPI_GITHUB_CONNECTION_ID"])
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
		{name: "user repository uses broker", input: entities.StartRequest{Scope: entities.ScopeUser, Params: &entities.SessionParams{}}, repo: "acme/repo", want: true},
		{name: "user explicit connection uses broker", input: entities.StartRequest{Scope: entities.ScopeUser, Params: &entities.SessionParams{ConnectionID: "connection-1"}}, repo: "acme/repo", want: true},
		{name: "user explicit token wins", input: entities.StartRequest{Scope: entities.ScopeUser, Params: &entities.SessionParams{GithubToken: "legacy-token"}}, repo: "acme/repo", want: false},
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

func TestExcludeTemplatedSessions(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "templated-public-id"},
		&sessionListTestSession{id: "templated-runtime-id"},
		&sessionListTestSession{id: "active-id"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "templated-public-id", RemoteSessionID: "templated-runtime-id", Status: "templated"},
		{SessionID: "active-id", Status: "active"},
	}

	got := excludeTemplatedSessions(sessions, routes)
	require.Len(t, got, 1)
	require.Equal(t, "active-id", got[0].ID())
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

func TestApplyContextTemplateLaunchRestoresCompatibleSourceSettings(t *testing.T) {
	req := &entities.StartRequest{
		Tags:   map[string]string{"caller": "kept", "repository": "wrong/repository"},
		Params: &entities.SessionParams{Message: "continue"},
	}
	template := &entities.SessionContextTemplate{
		AgentType:        "codex-acp",
		SessionProfileID: "profile-source",
		Tags: map[string]string{
			"repository": "owner/source",
			"branch":     "feature/context",
		},
	}

	applyContextTemplateLaunch(req, template)

	require.Equal(t, "owner/source", req.Tags["repository"])
	require.Equal(t, "feature/context", req.Tags["branch"])
	require.Equal(t, "kept", req.Tags["caller"])
	require.Equal(t, "profile-source", req.SessionProfileID)
	require.Equal(t, "codex-acp", req.Params.AgentType)
	require.Equal(t, "continue", req.Params.Message)
}
