package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"k8s.io/client-go/kubernetes/fake"
)

func TestValidateGoogleClaims(t *testing.T) {
	connection := googleConnection{HostedDomains: []string{"example.com"}, EmailDomains: []string{"example.com"}}
	require.NoError(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@example.com", EmailVerified: true, HostedDomain: "example.com"}))
	require.Error(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@other.test", EmailVerified: true, HostedDomain: "other.test"}))
	require.Error(t, validateGoogleClaims(connection, &googleClaims{Subject: "subject-1", Email: "user@example.com", HostedDomain: "example.com"}))
}

func TestGoogleAuthorizationURLUsesOIDCSecurityParameters(t *testing.T) {
	connection := googleConnection{OAuthClientID: "client-id", HostedDomains: []string{"example.com"}}
	state := googleOAuthState{ID: "state-id", CallbackURL: "https://ccplant.example/api/v1/auth/google-connections/callback", Nonce: "nonce", PKCEVerifier: "verifier"}
	authURL, err := url.Parse(googleAuthorizationURL(connection, state))
	require.NoError(t, err)
	require.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", authURL.Scheme+"://"+authURL.Host+authURL.Path)
	require.Equal(t, "openid email profile", authURL.Query().Get("scope"))
	require.Equal(t, "nonce", authURL.Query().Get("nonce"))
	require.Equal(t, "S256", authURL.Query().Get("code_challenge_method"))
	require.NotEmpty(t, authURL.Query().Get("code_challenge"))
	require.Equal(t, "example.com", authURL.Query().Get("hd"))
}

func TestGoogleIdentitySecretNameDoesNotExposeSubject(t *testing.T) {
	name := googleIdentitySecretName("connection", "sensitive-subject")
	require.NotContains(t, name, "sensitive-subject")
	require.Equal(t, name, googleIdentitySecretName("connection", "sensitive-subject"))
}

func TestGoogleLoginUsesApplicationUserIDFromLinkedPrincipal(t *testing.T) {
	t.Parallel()
	principals := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	principal, err := principals.getOrCreatePrincipal(context.Background(), "github:42")
	require.NoError(t, err)
	principal, err = principals.bindPrincipalToApplicationUser(context.Background(), "github:42", principal, "alice")
	require.NoError(t, err)
	require.Equal(t, "alice", applicationUserID(principal))

	reloaded, err := principals.loadPrincipal(context.Background(), "github:42")
	require.NoError(t, err)
	require.Equal(t, "alice", applicationUserID(reloaded))

	googleSessionUser := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice@example.com")
	resolved, err := principals.loadPrincipalForUser(context.Background(), googleSessionUser)
	require.NoError(t, err)
	require.Equal(t, principal.ID, resolved.ID)
}

func TestGoogleConnectionLoginCreatesLinkedIdentity(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	principals := NewGitHubConnectionsController(client, "test", "https://service.example.com")
	controller := NewGoogleConnectionsController(client, "test", "https://service.example.com", true, principals)
	connection := googleConnection{ID: "workspace", Name: "Google Workspace", AllowUserCreation: true}
	claims := &googleClaims{
		Subject:       "subject-1",
		Email:         "alice@example.com",
		EmailVerified: true,
		Name:          "Alice",
		Picture:       "https://example.com/alice.png",
		HostedDomain:  "example.com",
	}

	principal, err := controller.resolveLoginPrincipal(context.Background(), connection, claims)
	require.NoError(t, err)

	var identity googleIdentity
	_, err = principals.loadObject(context.Background(), googleIdentitySecretName(connection.ID, claims.Subject), &identity)
	require.NoError(t, err)
	require.Equal(t, principal.ID, identity.PrincipalID)
	require.Equal(t, connection.ID, identity.ConnectionID)
	require.Equal(t, claims.Email, identity.Email)
}

func TestCannotUnlinkGoogleConnectionUsedByCurrentLogin(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	principals := NewGitHubConnectionsController(client, "test", "https://service.example.com")
	controller := NewGoogleConnectionsController(client, "test", "https://service.example.com", true, principals)
	connection := googleConnection{ID: "workspace", Name: "Google Workspace", AllowUserCreation: true}
	claims := &googleClaims{Subject: "subject-1", Email: "alice@example.com", EmailVerified: true}
	principal, err := controller.resolveLoginPrincipal(context.Background(), connection, claims)
	require.NoError(t, err)

	var identity googleIdentity
	_, err = principals.loadObject(context.Background(), googleIdentitySecretName(connection.ID, claims.Subject), &identity)
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/users/me/google-identities/"+identity.ID, nil)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.SetParamNames("identity_id")
	ctx.SetParamValues(identity.ID)
	ctx.Set("internal_user", entities.NewUser(entities.UserID(principal.ID), entities.UserTypeRegular, claims.Email))
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindAPIKey, Provider: "google", ConnectionID: connection.ID})

	err = controller.Unlink(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}

func TestApplicationUserIDFallsBackForExistingPrincipalRecords(t *testing.T) {
	t.Parallel()
	require.Equal(t, "local-user", applicationUserID(githubPrincipal{ID: "principal-id", InternalUserID: "internal:local-user"}))
	require.Equal(t, "principal-id", applicationUserID(githubPrincipal{ID: "principal-id", InternalUserID: "github:42"}))
}

func TestLoadPrincipalForUserBackfillsApplicationUserID(t *testing.T) {
	t.Parallel()
	principals := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	principal, err := principals.getOrCreatePrincipal(context.Background(), "internal:legacy-user")
	require.NoError(t, err)
	require.Empty(t, principal.ApplicationUserID)

	user := entities.NewUser(entities.UserID("legacy-user"), entities.UserTypeRegular, "legacy-user")
	loaded, err := principals.loadPrincipalForUser(context.Background(), user)
	require.NoError(t, err)
	require.Equal(t, "legacy-user", loaded.ApplicationUserID)
}
