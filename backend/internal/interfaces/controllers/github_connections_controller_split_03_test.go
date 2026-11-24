package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLinkIdentityIsIdempotentAndRejectsAnotherPrincipal(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	identity := githubIdentity{ID: "identity-1", PrincipalID: "principal-1", ConnectionID: "connection-1", GitHubUserID: 42, Login: "alice"}

	created, err := controller.linkIdentity(context.Background(), identity, "token-1", nil)
	require.NoError(t, err)
	require.True(t, created)
	created, err = controller.linkIdentity(context.Background(), identity, "token-2", nil)
	require.NoError(t, err)
	require.False(t, created)
	secret, err := controller.client.CoreV1().Secrets("test").Get(context.Background(), identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "token-2", string(secret.Data[githubAccessTokenKey]))
	require.NotContains(t, string(secret.Data["record.json"]), "token-2")

	identity.PrincipalID = "principal-2"
	created, err = controller.linkIdentity(context.Background(), identity, "token-3", nil)
	require.False(t, created)
	require.ErrorIs(t, err, errIdentityConflict)
}

func TestResolveAccessTokenChecksOwnershipAndExpiry(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "", true)
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "internal:alice")
	require.NoError(t, err)
	identity := githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: "connection-1", GitHubUserID: 42, Login: "alice"}
	expiresAt := time.Now().UTC().Add(time.Hour)
	created, err := controller.linkIdentity(context.Background(), identity, "oauth-token", &expiresAt)
	require.NoError(t, err)
	require.True(t, created)
	token, err := controller.ResolveAccessToken(context.Background(), user, "connection-1")
	require.NoError(t, err)
	require.Equal(t, "oauth-token", token)

	token, err = controller.ResolveAccessToken(context.Background(), user, "connection-2")
	require.Empty(t, token)
	require.ErrorContains(t, err, "not linked")
	expiresAt = time.Now().UTC().Add(-time.Minute)
	created, err = controller.linkIdentity(context.Background(), identity, "expired-token", &expiresAt)
	require.NoError(t, err)
	require.False(t, created)
	token, err = controller.ResolveAccessToken(context.Background(), user, "connection-1")
	require.Empty(t, token)
	require.ErrorContains(t, err, "expired")
}

func TestSanitizeReturnTo(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/settings/personal/account-connections?tab=github", sanitizeReturnTo("/settings/personal/account-connections?tab=github"))
	require.Equal(t, "/settings/personal/account-connections", sanitizeReturnTo("https://evil.example.com"))
	require.Equal(t, "/settings/personal/account-connections", sanitizeReturnTo("//evil.example.com"))
}

func TestResolveGitHubConnectionCallbackURLsAcceptAPIV1(t *testing.T) {
	t.Setenv("OAUTH_ALLOWED_REDIRECT_URIS", "https://ui.example.test/api/auth/github/callback")
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/users/me/github-identities/link", nil)
	req.Header.Set("Origin", "https://ui.example.test")
	ctx := e.NewContext(req, httptest.NewRecorder())
	callbackURL := "https://ui.example.test/api/v1/auth/github-connections/callback"

	resolved, err := controller.resolveCallbackURL(ctx, callbackURL)
	require.NoError(t, err)
	require.Equal(t, callbackURL, resolved)

	resolved, err = controller.resolveLoginCallbackURL(ctx, callbackURL)
	require.NoError(t, err)
	require.Equal(t, callbackURL, resolved)
}

func TestResolveGitHubConnectionCallbackURLsRejectDifferentOrigin(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/users/me/github-identities/link", nil)
	req.Header.Set("Origin", "https://ui.example.test")
	ctx := e.NewContext(req, httptest.NewRecorder())
	callbackURL := "https://evil.example.test/api/v1/auth/github-connections/callback"

	resolved, err := controller.resolveCallbackURL(ctx, callbackURL)
	require.Empty(t, resolved)
	require.Error(t, err)
	resolved, err = controller.resolveLoginCallbackURL(ctx, callbackURL)
	require.Empty(t, resolved)
	require.Error(t, err)
}

func TestNormalizeOAuthScope(t *testing.T) {
	t.Parallel()
	require.Equal(t, "read:user read:org project", normalizeOAuthScope(""))
	require.Equal(t, "read:user repo", normalizeOAuthScope("  read:user   repo  "))
}

func TestShowConnectionOnLoginDefaultsToTrue(t *testing.T) {
	t.Parallel()
	require.True(t, showConnectionOnLogin(githubConnection{}))
	visible := true
	hidden := false
	require.True(t, showConnectionOnLogin(githubConnection{ShowOnLogin: &visible}))
	require.False(t, showConnectionOnLogin(githubConnection{ShowOnLogin: &hidden}))
}

func TestNormalizeOrganizations(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"another-org", "example-org"}, normalizeOrganizations([]string{" Example-Org ", "example-org", "another-org", ""}))
}

func TestOrganizationAssignmentMustBeUnique(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	require.NoError(t, controller.saveConnection(context.Background(), githubConnection{ID: "first", Name: "First", Organizations: []string{"example-org"}}, "", ""))
	require.ErrorContains(t, controller.validateOrganizationAssignments(context.Background(), "second", []string{"EXAMPLE-ORG"}), "already assigned")
	require.NoError(t, controller.validateOrganizationAssignments(context.Background(), "first", []string{"example-org"}))
}

func TestResolveAccessTokenForOrganization(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "", true)
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "internal:alice")
	require.NoError(t, err)
	connection := githubConnection{ID: "corp", Name: "Corp", Enabled: true, BaseURL: "https://github.corp.example", APIURL: "https://github.corp.example/api/v3", Organizations: []string{"example-org"}}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	linked, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice"}, "corp-token", nil)
	require.NoError(t, err)
	require.True(t, linked)

	token, connectionID, matched, err := controller.ResolveAccessTokenForOrganization(context.Background(), user, "Example-Org")
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "corp-token", token)
	require.Equal(t, "corp", connectionID)
	baseURL, apiURL, err := controller.ResolveConnectionURLs(context.Background(), connectionID)
	require.NoError(t, err)
	require.Equal(t, "https://github.corp.example", baseURL)
	require.Equal(t, "https://github.corp.example/api/v3", apiURL)
	token, connectionID, matched, err = controller.ResolveAccessTokenForOrganization(context.Background(), user, "unmapped-org")
	require.NoError(t, err)
	require.Empty(t, token)
	require.False(t, matched)
	require.Empty(t, connectionID)
}
