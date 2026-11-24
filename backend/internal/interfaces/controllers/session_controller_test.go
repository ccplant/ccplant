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

type githubRoutingResolver struct {
	githubConnectionURLResolverStub
	token        string
	connectionID string
	matched      bool
	err          error
}

func (r githubRoutingResolver) ResolveAccessToken(context.Context, *entities.User, string) (string, error) {
	return r.token, r.err
}

func (r githubRoutingResolver) ResolveAccessTokenForOrganization(context.Context, *entities.User, string) (string, string, bool, error) {
	return r.token, r.connectionID, r.matched, r.err
}

type githubRoutingProfileRepo struct {
	repositories.SessionProfileRepository
	profile *entities.SessionProfile
}

type missingSettingsRepo struct {
	repositories.SettingsRepository
}

func (missingSettingsRepo) Exists(context.Context, string) (bool, error) { return false, nil }

type githubSettingsRepo struct {
	repositories.SettingsRepository
	settings *entities.Settings
}

func (r githubSettingsRepo) Exists(_ context.Context, name string) (bool, error) {
	return r.settings != nil && r.settings.Name() == name, nil
}

func (r githubSettingsRepo) FindByName(_ context.Context, name string) (*entities.Settings, error) {
	if r.settings == nil || r.settings.Name() != name {
		return nil, errors.New("settings not found")
	}
	return r.settings, nil
}

func (r githubRoutingProfileRepo) Get(context.Context, string) (*entities.SessionProfile, error) {
	return r.profile, nil
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

func (s githubConnectionURLResolverStub) IssueBrokerLeaseForRepositories(context.Context, string, string, string, string, []string, *entities.User) (string, string, bool, error) {
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
	request      entities.StartRequest
}

func (p *previewSessionCreator) CreateSession(context.Context, string, entities.StartRequest, string, string, []string) (entities.Session, error) {
	p.createCalled = true
	return nil, errors.New("CreateSession must not be called by dry-run")
}

func (*previewSessionCreator) DeleteSessionByID(string) error { return nil }

func (p *previewSessionCreator) PreviewSession(_ context.Context, _ string, req entities.StartRequest, _ string, _ string, _ []string) (*entities.SessionStartPreview, error) {
	p.request = req
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
	resolution := body["resolution"].(map[string]interface{})
	require.Equal(t, "reuse_not_requested", resolution["reuse"].(map[string]interface{})["reason"])
	effective := body["effective_request"].(map[string]interface{})
	require.Equal(t, "<redacted>", effective["environment"].(map[string]interface{})["CUSTOM"])
	require.Equal(t, "<redacted>", effective["params"].(map[string]interface{})["github_token"])
	settings := body["settings"].(map[string]interface{})
	require.Equal(t, "<redacted>", settings["env"].(map[string]interface{})["VISIBLE_NAME"])
	require.Equal(t, "<redacted>", settings["credentials"])
}

func TestStartSessionFallsBackToBuiltInGitHubTokenForUnlinkedOrganizationConnection(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user","tags":{"repository":"acme/repo"}}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanCreate: true, CanRead: true},
	})
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "built-in-token"})

	creator := &previewSessionCreator{}
	controller := NewSessionController(nil, creator, WithGitHubTokenResolver(githubRoutingResolver{}))

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "built-in-token", creator.request.Params.GithubToken)
	require.Empty(t, creator.request.Params.ConnectionID)
}

func TestStartPersonalSessionWithoutRepositoryUsesBuiltInGitHubToken(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanCreate: true, CanRead: true},
	})
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "built-in-token"})

	creator := &previewSessionCreator{}
	controller := NewSessionController(nil, creator, WithGitHubTokenResolver(githubRoutingResolver{}))

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "built-in-token", creator.request.Params.GithubToken)
	require.Empty(t, creator.request.Params.ConnectionID)
	require.NotContains(t, creator.request.Environment, "AGENTAPI_GITHUB_BROKER_TOKEN")
}

func TestStartPersonalSessionUsesAuthenticatedConnectionBeforeOrganizationBroker(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user","tags":{"repository":"acme/repo"}}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanCreate: true, CanRead: true},
	})
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "personal-token", ConnectionID: "personal-connection"})

	creator := &previewSessionCreator{}
	resolver := githubRoutingResolver{
		githubConnectionURLResolverStub: githubConnectionURLResolverStub{baseURL: "https://github.com", apiURL: "https://api.github.com"},
		token:                           "connection-token", connectionID: "organization-connection", matched: true,
	}
	controller := NewSessionController(nil, creator, WithGitHubTokenResolver(resolver))

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Nil(t, creator.request.Params)
	require.Equal(t, "<generated-at-apply>", creator.request.Environment["AGENTAPI_GITHUB_BROKER_TOKEN"])
	require.Equal(t, "personal-connection", creator.request.Environment["AGENTAPI_GITHUB_CONNECTION_ID"])
	require.Equal(t, "https://github.com", creator.request.Environment["GITHUB_URL"])
	require.Equal(t, "https://api.github.com", creator.request.Environment["GITHUB_API"])
	require.Equal(t, "github.com", creator.request.Environment["GH_HOST"])
}

func TestStartSessionUsesProfileConnectionBeforeBuiltInGitHubToken(t *testing.T) {
	profile := entities.NewSessionProfile("profile", "Profile", "alice")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{ConnectionID: "profile-connection"})
	profile.SetConfig(cfg)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start?dry_run=true", strings.NewReader(`{"scope":"user","session_profile_id":"profile"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User:          entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice"),
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanCreate: true, CanRead: true},
	})
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "built-in-token", ConnectionID: "built-in"})

	creator := &previewSessionCreator{}
	resolver := githubRoutingResolver{
		githubConnectionURLResolverStub: githubConnectionURLResolverStub{baseURL: "https://github.example.com", apiURL: "https://github.example.com/api/v3"},
		token:                           "profile-token",
	}
	controller := NewSessionController(nil, creator,
		WithGitHubTokenResolver(resolver),
		WithSessionProfileRepository(githubRoutingProfileRepo{profile: profile}),
	)

	require.NoError(t, controller.StartSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "profile-token", creator.request.Params.GithubToken)
	require.Equal(t, "profile-connection", creator.request.Params.ConnectionID)
	require.Equal(t, "github.example.com", creator.request.Environment["GH_HOST"])
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
		name           string
		scope          entities.ResourceScope
		existingToken  string
		credential     *auth.CredentialContext
		wantToken      string
		wantConnection string
	}{
		{name: "user scope receives authenticated GitHub token", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: "oauth-token"},
		{name: "connection credential carries verified routing", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "ghec-token", ConnectionID: "ghec"}, wantToken: "ghec-token", wantConnection: "ghec"},
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
			if startReq.Params.ConnectionID != tt.wantConnection {
				t.Fatalf("ConnectionID = %q, want %q", startReq.Params.ConnectionID, tt.wantConnection)
			}
		})
	}
}
