package controllers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

func TestCreateConnectionRequiresEncryptedKVForStoredSecret(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	body := map[string]any{
		"name": "corp", "base_url": "https://github.example.com", "api_url": "https://github.example.com/api/v3", "oauth_client_id": "client",
		"oauth_client_secret": map[string]any{"source": "encrypted", "value": "secret"},
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/github-connections", bytes.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	err = controller.Create(e.NewContext(req, recorder))
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestCreateConnectionStoresSecretWithKubernetesBackend(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	controller := NewGitHubConnectionsController(client, "test", "", true)
	body := map[string]any{
		"name": "corp", "base_url": "https://github.example.com", "api_url": "https://github.example.com/api/v3", "oauth_client_id": "client",
		"oauth_client_secret": map[string]any{"source": "encrypted", "value": "super-sensitive-value"},
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/admin/github-connections", bytes.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	require.NoError(t, controller.Create(e.NewContext(req, recorder)))
	require.Equal(t, http.StatusCreated, recorder.Code)

	connections, err := client.CoreV1().Secrets("test").List(context.Background(), metav1.ListOptions{LabelSelector: githubConnectionLabel + "=true"})
	require.NoError(t, err)
	require.Len(t, connections.Items, 1)
	require.Equal(t, "super-sensitive-value", string(connections.Items[0].Data["client_secret"]))
	require.NotContains(t, string(connections.Items[0].Data["record.json"]), "super-sensitive-value")
}

func TestNormalizeGitHubURL(t *testing.T) {
	t.Parallel()

	got, err := normalizeGitHubURL(" https://github.example.com/api/v3/ ")
	require.NoError(t, err)
	require.Equal(t, "https://github.example.com/api/v3", got)

	for _, raw := range []string{"http://github.example.com", "https://user@example.com", "https://example.com?q=x", "//example.com"} {
		_, err := normalizeGitHubURL(raw)
		require.Error(t, err, raw)
	}
}

func TestValidateGitHubSecret(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateGitHubSecret("encrypted", "secret", ""))
	require.NoError(t, validateGitHubSecret("environment", "", "GITHUB_OAUTH_CORP_CLIENT_SECRET"))
	require.Error(t, validateGitHubSecret("encrypted", "", ""))
	require.Error(t, validateGitHubSecret("environment", "", "DATABASE_PASSWORD"))
}

func TestGitHubAppPrivateKeyAndBrokerRefresh(t *testing.T) {
	t.Parallel()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemValue := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	var installationCalls, tokenCalls int
	githubAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/acme/payments/installation":
			installationCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 99})
		case "/api/v3/app/installations/99/access_tokens":
			tokenCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "installation-token", "expires_at": time.Now().UTC().Add(time.Hour)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubAPI.Close()

	client := fake.NewSimpleClientset()
	controller := NewGitHubConnectionsController(client, "test", "", true)
	connection := githubConnection{ID: "connection-1", Name: "GitHub", BaseURL: githubAPI.URL, APIURL: githubAPI.URL, Enabled: true, Organizations: []string{"acme"}, GitHubApp: &githubAppConfiguration{AppID: 123}}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	_, _, resourceVersion, err := controller.loadConnection(context.Background(), connection.ID)
	require.NoError(t, err)
	require.NoError(t, controller.saveGitHubAppPrivateKey(context.Background(), connection, pemValue, resourceVersion))

	lease, connectionID, matched, err := controller.IssueBrokerLeaseForOrganization(context.Background(), "session-1", "acme", "acme/payments")
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, connection.ID, connectionID)
	require.NotEmpty(t, lease)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-1/github-credentials", nil)
	req.Header.Set("Authorization", "Bearer "+lease)
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(req, recorder)
	ctx.SetPath("/internal/sessions/:sessionId/github-credentials")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-1")
	require.NoError(t, controller.BrokerCredentials(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "installation-token")
	require.Equal(t, 1, installationCalls)
	require.Equal(t, 1, tokenCalls)
	require.NoError(t, controller.RevokeBrokerLeases(context.Background(), "session-1"))
	recorder = httptest.NewRecorder()
	ctx = e.NewContext(req, recorder)
	ctx.SetPath("/internal/sessions/:sessionId/github-credentials")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-1")
	err = controller.BrokerCredentials(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusUnauthorized, httpErr.Code)

	stored, err := client.CoreV1().Secrets("test").Get(context.Background(), connectionSecretName(connection.ID), metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, pemValue, stored.Data[githubAppPrivateKeyKey])
	require.NotContains(t, string(stored.Data["record.json"]), string(pemValue))
}

func TestValidateGitHubAppPrivateKey(t *testing.T) {
	t.Parallel()
	require.Error(t, validateGitHubAppPrivateKey([]byte("not pem")))
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	value := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	require.NoError(t, validateGitHubAppPrivateKey(value))
}

func TestPrincipalIsStableAndRandom(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")

	first, err := controller.getOrCreatePrincipal(context.Background(), "alice")
	require.NoError(t, err)
	second, err := controller.getOrCreatePrincipal(context.Background(), "alice")
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.NotEqual(t, "alice", first.ID)
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

func TestResolveLoginPrincipalUsesLinkedPrincipalID(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github:1")
	require.NoError(t, err)
	connection := githubConnection{ID: "enterprise", BaseURL: "https://github.example.com", APIURL: "https://github.example.com/api/v3"}
	identity := githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice-enterprise"}
	_, err = controller.linkIdentity(context.Background(), identity, "token", nil)
	require.NoError(t, err)

	resolved, err := controller.resolveLoginPrincipal(context.Background(), connection, githubOAuthUser{ID: 42, Login: "alice-enterprise"}, "token", nil)
	require.NoError(t, err)
	require.Equal(t, principal.ID, resolved.ID)
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
	_, err = controller.loadObject(context.Background(), identitySecretName(connection.ID, 42), &identity)
	require.NoError(t, err)
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
	_, err = controller.linkIdentity(context.Background(), githubIdentity{ID: "enterprise-identity", PrincipalID: legacy.ID, ConnectionID: enterpriseConnection.ID, GitHubUserID: 42, Login: "alice-enterprise"}, "enterprise-token", nil)
	require.NoError(t, err)

	resolved, err := controller.resolveLoginPrincipal(context.Background(), enterpriseConnection, githubOAuthUser{ID: 42, Login: "alice-enterprise"}, "enterprise-token", nil)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, resolved.ID)
}

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
	_, err = controller.linkIdentity(context.Background(), identity, "token-3", nil)
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
	_, err = controller.linkIdentity(context.Background(), identity, "oauth-token", &expiresAt)
	require.NoError(t, err)
	token, err := controller.ResolveAccessToken(context.Background(), user, "connection-1")
	require.NoError(t, err)
	require.Equal(t, "oauth-token", token)

	_, err = controller.ResolveAccessToken(context.Background(), user, "connection-2")
	require.ErrorContains(t, err, "not linked")
	expiresAt = time.Now().UTC().Add(-time.Minute)
	_, err = controller.linkIdentity(context.Background(), identity, "expired-token", &expiresAt)
	require.NoError(t, err)
	_, err = controller.ResolveAccessToken(context.Background(), user, "connection-1")
	require.ErrorContains(t, err, "expired")
}

func TestSanitizeReturnTo(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/settings/personal/account-connections?tab=github", sanitizeReturnTo("/settings/personal/account-connections?tab=github"))
	require.Equal(t, "/settings/personal/account-connections", sanitizeReturnTo("https://evil.example.com"))
	require.Equal(t, "/settings/personal/account-connections", sanitizeReturnTo("//evil.example.com"))
}

func TestResolveGitHubConnectionCallbackURLsAcceptAPIV1(t *testing.T) {
	t.Parallel()
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

	_, err := controller.resolveCallbackURL(ctx, callbackURL)
	require.Error(t, err)
	_, err = controller.resolveLoginCallbackURL(ctx, callbackURL)
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
	_, err = controller.linkIdentity(context.Background(), githubIdentity{ID: "identity-1", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice"}, "corp-token", nil)
	require.NoError(t, err)

	token, connectionID, matched, err := controller.ResolveAccessTokenForOrganization(context.Background(), user, "Example-Org")
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "corp-token", token)
	require.Equal(t, "corp", connectionID)
	baseURL, apiURL, err := controller.ResolveConnectionURLs(context.Background(), connectionID)
	require.NoError(t, err)
	require.Equal(t, "https://github.corp.example", baseURL)
	require.Equal(t, "https://github.corp.example/api/v3", apiURL)
	_, connectionID, matched, err = controller.ResolveAccessTokenForOrganization(context.Background(), user, "unmapped-org")
	require.NoError(t, err)
	require.False(t, matched)
	require.Empty(t, connectionID)
}
