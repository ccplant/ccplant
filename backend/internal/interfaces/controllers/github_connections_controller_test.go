package controllers

import (
	"bytes"
	"context"
	"encoding/json"
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
