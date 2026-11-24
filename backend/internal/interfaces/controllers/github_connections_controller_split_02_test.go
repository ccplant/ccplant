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
	testassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRepairPrincipalClearsApplicationUserIDAndAttachesIdentity(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "public", Name: "GitHub", BaseURL: "https://github.com", APIURL: "https://api.github.com"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "secret", ""))
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:public:1")
	require.NoError(t, err)
	principal, err = controller.bindPrincipalToApplicationUser(context.Background(), principal.InternalUserID, principal, "obsolete-user")
	require.NoError(t, err)

	body := `{"clear_application_user_id":true,"identities":[{"connection_id":"public","github_user_id":42,"login":"alice"}]}`
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/principals/"+principal.ID+"/repair", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("principalId")
	ctx.SetParamValues(principal.ID)
	require.NoError(t, controller.RepairPrincipal(ctx))
	require.Equal(t, http.StatusOK, rec.Code)

	repaired, err := controller.loadPrincipal(context.Background(), principal.InternalUserID)
	require.NoError(t, err)
	require.Empty(t, repaired.ApplicationUserID)
	var identity githubIdentity
	identitySecret, err := controller.loadObject(context.Background(), identitySecretName("public", 42), &identity)
	require.NoError(t, err)
	require.NotNil(t, identitySecret)
	require.Equal(t, principal.ID, identity.PrincipalID)
	require.Equal(t, "alice", identity.Login)
}

func TestRepairPrincipalRequiresExplicitIdentityReassignment(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "public", Name: "GitHub", BaseURL: "https://github.com", APIURL: "https://api.github.com"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "secret", ""))
	target, err := controller.getOrCreatePrincipal(context.Background(), "github:1")
	require.NoError(t, err)
	source, err := controller.getOrCreatePrincipal(context.Background(), "github:2")
	require.NoError(t, err)
	created, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "identity", PrincipalID: source.ID, ConnectionID: "public", GitHubUserID: 42, Login: "alice"}, "token", nil)
	require.NoError(t, err)
	require.True(t, created)

	body := `{"identities":[{"connection_id":"public","github_user_id":42}]}`
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/principals/"+target.ID+"/repair", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.SetParamNames("principalId")
	ctx.SetParamValues(target.ID)
	err = controller.RepairPrincipal(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestLegacyGitHubLoginResolvesStablePrincipalID(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	first, err := controller.ResolvePrincipalIDForGitHubUser(context.Background(), 42)
	require.NoError(t, err)
	second, err := controller.ResolvePrincipalIDForGitHubUser(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEqual(t, "alice", first)
}

func TestLegacyGitHubLoginUsesLoginAsApplicationUserID(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")

	first, err := controller.ResolveApplicationUserIDForGitHubUser(context.Background(), 42, "alice")
	require.NoError(t, err)
	second, err := controller.ResolveApplicationUserIDForGitHubUser(context.Background(), 42, "alice-renamed")
	require.NoError(t, err)
	require.Equal(t, "alice", first)
	require.Equal(t, first, second, "the personal resource owner must remain stable after a GitHub login rename")
}

func TestConnectedGitHubLoginUsesLinkedApplicationUserID(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "public", Name: "GitHub", BaseURL: "https://github.com", APIURL: "https://api.github.com"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "client-secret", ""))
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:public:42")
	require.NoError(t, err)
	created, err := controller.linkIdentity(context.Background(), githubIdentity{
		ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice",
	}, "token", nil)
	require.NoError(t, err)
	require.True(t, created)

	got, err := controller.ResolveApplicationUserIDForGitHubUser(context.Background(), 42, "alice")
	require.NoError(t, err)
	require.Equal(t, principal.ID, got)
}

func TestGitHubConnectionCredentialIsBoundToConnection(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com", true)
	connection := githubConnection{ID: "ghec", Name: "GHEC", BaseURL: "https://github.com", APIURL: "https://api.github.com", Enabled: true, SecretSource: "encrypted"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "ghec-client-secret", ""))

	credential, err := controller.IssueGitHubConnectionCredential(context.Background(), connection.ID, "ghec-access-token")
	require.NoError(t, err)
	require.NotContains(t, credential, "ghec-access-token")

	resolved, err := controller.ResolveGitHubConnectionCredential(context.Background(), credential)
	require.NoError(t, err)
	require.Equal(t, connection.ID, resolved.ConnectionID)
	require.Equal(t, connection.APIURL, resolved.APIURL)
	require.Equal(t, "ghec-access-token", resolved.AccessToken)

	tamperedConnection := githubConnection{ID: "ghes", Name: "GHES", BaseURL: "https://ghes.example.com", APIURL: "https://ghes.example.com/api/v3", Enabled: true, SecretSource: "encrypted"}
	require.NoError(t, controller.saveConnection(context.Background(), tamperedConnection, "ghes-client-secret", ""))
	tampered := strings.Replace(credential, "Z2hlYw", "Z2hlcw", 1)
	tamperedCredential, err := controller.ResolveGitHubConnectionCredential(context.Background(), tampered)
	require.Error(t, err)
	require.Empty(t, tamperedCredential)
}

func TestBuiltInTeamRequestsUseConfiguredEnterpriseAPIURL(t *testing.T) {
	t.Parallel()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		require.Equal(t, "Bearer enterprise-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v3/user/teams":
			testassert.NoError(t, json.NewEncoder(w).Encode([]map[string]any{{
				"slug":         "platform",
				"name":         "Platform",
				"organization": map[string]any{"login": "acme"},
			}}))
		case "/api/v3/orgs/acme/teams/platform/members":
			testassert.NoError(t, json.NewEncoder(w).Encode([]githubOAuthUser{{ID: 42, Login: "alice"}}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	controller.httpClient = server.Client()
	controller.SetBuiltInAPIURL(server.URL + "/api/v3/")

	teams, err := controller.FetchBuiltInTeams(context.Background(), "enterprise-token")
	require.NoError(t, err)
	require.Equal(t, []auth.GitHubTeamMembership{{Organization: "acme", TeamSlug: "platform", TeamName: "Platform"}}, teams)

	members, err := controller.FetchExternalTeamMembers(context.Background(), "principal-1", entities.ExternalTeamBinding{Organization: "acme", TeamSlug: "platform"}, "enterprise-token")
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, int64(42), members[0].GitHubUserID)
	require.Equal(t, []string{
		"/api/v3/user/teams?per_page=100&page=1",
		"/api/v3/orgs/acme/teams/platform/members?role=all&per_page=100&page=1",
	}, requested)
}

func TestResolveLoginPrincipalUsesLinkedPrincipalID(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github:1")
	require.NoError(t, err)
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com", APIURL: "https://github.example.com/api/v3"}
	identity := githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice-enterprise"}
	created, err := controller.linkIdentity(context.Background(), identity, "token", nil)
	require.NoError(t, err)
	require.True(t, created)

	resolved, err := controller.resolveLoginPrincipal(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice-enterprise"}, "token", nil)
	require.NoError(t, err)
	require.Equal(t, principal.ID, resolved.ID)
}

func TestConnectionLoginCreatesLinkedIdentity(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{
		ID:                "enterprise",
		Name:              "Enterprise GitHub",
		BaseURL:           "https://github.example.com",
		APIURL:            "https://github.example.com/api/v3",
		AllowUserCreation: true,
	}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))

	principal, created, err := controller.resolveLoginPrincipalWithCreated(
		context.Background(),
		connection,
		githubOAuthUser{ID: 42, Login: "alice", AvatarURL: "https://github.example.com/alice.png"},
		"access-token",
		nil,
	)
	require.NoError(t, err)
	require.True(t, created)

	var identity githubIdentity
	identitySecret, err := controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.NoError(t, err)
	require.NotNil(t, identitySecret)
	require.Equal(t, principal.ID, identity.PrincipalID)
	require.Equal(t, connection.ID, identity.ConnectionID)
	require.Equal(t, "alice", identity.Login)
}

func TestCannotUnlinkConnectionUsedByCurrentLogin(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "enterprise", Name: "Enterprise GitHub", AllowUserCreation: true}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	principal, created, err := controller.resolveLoginPrincipalWithCreated(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice"}, "access-token", nil)
	require.NoError(t, err)
	require.True(t, created)

	var identity githubIdentity
	identitySecret, err := controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.NoError(t, err)
	require.NotNil(t, identitySecret)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/users/me/github-identities/"+identity.ID, nil)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.SetParamNames("identity_id")
	ctx.SetParamValues(identity.ID)
	ctx.Set("internal_user", entities.NewUser(entities.UserID(principal.ID), entities.UserTypeRegular, "alice"))
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, ConnectionID: connection.ID})

	err = controller.Unlink(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestResolveLoginPrincipalLinksExistingBuiltInGHESPrincipal(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	controller.SetBuiltInAPIURL("https://github.example.com/api/v3")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github:42")
	require.NoError(t, err)
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com", APIURL: "https://github.example.com/api/v3"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))

	resolved, err := controller.resolveLoginPrincipal(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice"}, "token", nil)
	require.NoError(t, err)
	require.Equal(t, principal.ID, resolved.ID)

	var identity githubIdentity
	identitySecret, err := controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.NoError(t, err)
	require.NotNil(t, identitySecret)
	require.Equal(t, principal.ID, identity.PrincipalID)
}

func TestBuiltInGHESLoginUsesMatchingConnectionPrincipal(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	controller.SetBuiltInAPIURL("https://github.example.com/api/v3/")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:enterprise:42")
	require.NoError(t, err)
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com", APIURL: "https://github.example.com/api/v3"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	created, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice"}, "token", nil)
	require.NoError(t, err)
	require.True(t, created)

	resolved, err := controller.ResolvePrincipalIDForGitHubUser(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, principal.ID, resolved)
}

func TestBuiltInGHESDoesNotUseSameUserIDFromDifferentHost(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	controller.SetBuiltInAPIURL("https://first.example.com/api/v3")
	connectionPrincipal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:enterprise:42")
	require.NoError(t, err)
	connection := githubConnection{ID: "enterprise", BaseURL: "https://second.example.com", APIURL: "https://second.example.com/api/v3"}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	created, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "identity-1", PrincipalID: connectionPrincipal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice"}, "token", nil)
	require.NoError(t, err)
	require.True(t, created)

	resolved, err := controller.ResolvePrincipalIDForGitHubUser(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, connectionPrincipal.ID, resolved)
	builtInPrincipal, err := controller.loadPrincipal(context.Background(), "github:42")
	require.NoError(t, err)
	require.Equal(t, builtInPrincipal.ID, resolved)
}

func TestResolveLoginPrincipalRejectsUnlinkedUserWhenCreationDisabled(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com"}

	_, err := controller.resolveLoginPrincipal(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice"}, "token", nil)
	require.ErrorContains(t, err, "user creation is disabled")

	_, principalErr := controller.loadPrincipal(context.Background(), "github-connection:enterprise:42")
	require.Error(t, principalErr)
	var identity githubIdentity
	_, identityErr := controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.Error(t, identityErr)
}

func TestResolveLoginPrincipalCreatesUnlinkedUserWhenCreationEnabled(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com", AllowUserCreation: true}

	principal, err := controller.resolveLoginPrincipal(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice"}, "token", nil)
	require.NoError(t, err)
	require.NotEmpty(t, principal.ID)

	var identity githubIdentity
	identitySecret, err := controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.NoError(t, err)
	require.NotNil(t, identitySecret)
	require.Equal(t, principal.ID, identity.PrincipalID)
}

func TestResolveLoginPrincipalMigratesLegacyPrincipal(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	legacy := githubPrincipal{ID: "principal-1", InternalUserID: "github:1", CreatedAt: time.Now().UTC()}
	require.NoError(t, controller.createObject(context.Background(), principalSecretName(legacy.InternalUserID), githubPrincipalLabel, legacy, nil))
	publicConnection := githubConnection{ID: "public", Name: "GitHub", BaseURL: "https://github.com", APIURL: "https://api.github.com"}
	enterpriseConnection := githubConnection{ID: "enterprise", Name: "GHEC", BaseURL: "https://github.example.com", APIURL: "https://github.example.com/api/v3"}
	require.NoError(t, controller.saveConnection(context.Background(), publicConnection, "", ""))
	require.NoError(t, controller.saveConnection(context.Background(), enterpriseConnection, "", ""))
	_, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "public-identity", PrincipalID: legacy.ID, ConnectionID: publicConnection.ID, GitHubUserID: 1, Login: "alice"}, "public-token", nil)
	require.NoError(t, err)
	created, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "enterprise-identity", PrincipalID: legacy.ID, ConnectionID: enterpriseConnection.ID, GitHubUserID: 42, Login: "alice-enterprise"}, "enterprise-token", nil)
	require.NoError(t, err)
	require.True(t, created)

	resolved, err := controller.resolveLoginPrincipal(context.Background(), enterpriseConnection, githubOAuthUser{ID: 42, Login: "alice-enterprise"}, "enterprise-token", nil)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, resolved.ID)
}

func TestResolveTeamMembershipsMergesLinkedConnections(t *testing.T) {
	t.Parallel()
	teamServer := func(teams []map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/user/teams", r.URL.Path)
			require.NotEmpty(t, r.Header.Get("Authorization"))
			testassert.NoError(t, json.NewEncoder(w).Encode(teams))
		}))
	}
	first := teamServer([]map[string]any{
		{"slug": "developers", "name": "Developers", "permission": "push", "organization": map[string]any{"login": "Acme"}},
	})
	defer first.Close()
	second := teamServer([]map[string]any{
		{"slug": "developers", "name": "Developers", "permission": "push", "organization": map[string]any{"login": "acme"}},
		{"slug": "platform", "name": "Platform", "permission": "pull", "organization": map[string]any{"login": "Acme"}},
	})
	defer second.Close()

	client := fake.NewSimpleClientset()
	controller := NewGitHubConnectionsController(client, "test", "", true)
	controller.httpClient = first.Client()
	connections := []githubConnection{
		{ID: "ghes", Name: "GHES", BaseURL: first.URL, APIURL: first.URL, Enabled: true},
		{ID: "ghec", Name: "GHEC", BaseURL: second.URL, APIURL: second.URL, Enabled: true},
	}
	for i, connection := range connections {
		require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
		_, err := controller.linkIdentity(context.Background(), githubIdentity{ID: connection.ID + "-identity", PrincipalID: "principal-1", ConnectionID: connection.ID, GitHubUserID: int64(i + 1), Login: "alice"}, connection.ID+"-token", nil)
		require.NoError(t, err)
	}

	memberships, linked, err := controller.ResolveTeamMemberships(context.Background(), "principal-1")
	require.NoError(t, err)
	require.True(t, linked)
	require.Len(t, memberships, 2)
	require.Equal(t, "developers", memberships[0].TeamSlug)
	require.Equal(t, "platform", memberships[1].TeamSlug)
}
