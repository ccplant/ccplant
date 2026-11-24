package controllers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	testassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

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
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": 99}))
		case "/api/v3/repos/acme/shared/installation":
			installationCalls++
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": 99}))
		case "/api/v3/repos/acme/generated-one/installation":
			installationCalls++
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"id": 99}))
		case "/api/v3/app/installations/99/access_tokens":
			tokenCalls++
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"token": "installation-token", "expires_at": time.Now().UTC().Add(time.Hour)}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubAPI.Close()

	client := fake.NewSimpleClientset()
	controller := NewGitHubConnectionsController(client, "test", "", true)
	connection := githubConnection{ID: "connection-1", Name: "GitHub", BaseURL: githubAPI.URL, APIURL: githubAPI.URL, Enabled: true, Organizations: []string{"acme"}, GitHubApp: &githubAppConfiguration{AppID: 123}}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))
	loadedConnection, secret, resourceVersion, err := controller.loadConnection(context.Background(), connection.ID)
	require.NoError(t, err)
	require.Equal(t, connection.ID, loadedConnection.ID)
	require.Empty(t, secret)
	require.NoError(t, controller.saveGitHubAppPrivateKey(context.Background(), connection, pemValue, resourceVersion))

	lease, connectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "session-1", "", "acme", "acme/payments", []string{"acme/shared", "acme/generated-[a-z]+"}, nil)
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

	sharedReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-1/github-credentials?repository=acme%2Fshared", nil)
	sharedReq.Header.Set("Authorization", "Bearer "+lease)
	sharedRecorder := httptest.NewRecorder()
	sharedCtx := e.NewContext(sharedReq, sharedRecorder)
	sharedCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	sharedCtx.SetParamNames("sessionId")
	sharedCtx.SetParamValues("session-1")
	require.NoError(t, controller.BrokerCredentials(sharedCtx))
	require.Equal(t, http.StatusOK, sharedRecorder.Code)

	regexReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-1/github-credentials?repository=acme%2Fgenerated-one", nil)
	regexReq.Header.Set("Authorization", "Bearer "+lease)
	regexRecorder := httptest.NewRecorder()
	regexCtx := e.NewContext(regexReq, regexRecorder)
	regexCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	regexCtx.SetParamNames("sessionId")
	regexCtx.SetParamValues("session-1")
	require.NoError(t, controller.BrokerCredentials(regexCtx))
	require.Equal(t, http.StatusOK, regexRecorder.Code)
	require.Equal(t, 3, installationCalls)
	require.Equal(t, 3, tokenCalls)

	dynamicLease, connectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "session-dynamic", "", "", "", []string{"acme/shared"}, nil)
	require.NoError(t, err)
	require.True(t, matched)
	require.Empty(t, connectionID)
	require.NotEmpty(t, dynamicLease)

	dynamicReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-dynamic/github-credentials?repository=acme%2Fshared", nil)
	dynamicReq.Header.Set("Authorization", "Bearer "+dynamicLease)
	dynamicRecorder := httptest.NewRecorder()
	dynamicCtx := e.NewContext(dynamicReq, dynamicRecorder)
	dynamicCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	dynamicCtx.SetParamNames("sessionId")
	dynamicCtx.SetParamValues("session-dynamic")
	require.NoError(t, controller.BrokerCredentials(dynamicCtx))
	require.Equal(t, http.StatusOK, dynamicRecorder.Code)

	boundLease, connectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "session-bound", connection.ID, "", "", []string{"acme/shared"}, nil)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, connection.ID, connectionID)
	require.NotEmpty(t, boundLease)

	boundReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-bound/github-credentials?repository=acme%2Fshared", nil)
	boundReq.Header.Set("Authorization", "Bearer "+boundLease)
	boundRecorder := httptest.NewRecorder()
	boundCtx := e.NewContext(boundReq, boundRecorder)
	boundCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	boundCtx.SetParamNames("sessionId")
	boundCtx.SetParamValues("session-bound")
	require.NoError(t, controller.BrokerCredentials(boundCtx))
	require.Equal(t, http.StatusOK, boundRecorder.Code)

	boundPrimaryLease, connectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "session-bound-primary", connection.ID, "acme", "acme/payments", nil, nil)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, connection.ID, connectionID)
	require.NotEmpty(t, boundPrimaryLease)

	dynamicDeniedReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-dynamic/github-credentials?repository=acme%2Fsecret", nil)
	dynamicDeniedReq.Header.Set("Authorization", "Bearer "+dynamicLease)
	dynamicDeniedCtx := e.NewContext(dynamicDeniedReq, httptest.NewRecorder())
	dynamicDeniedCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	dynamicDeniedCtx.SetParamNames("sessionId")
	dynamicDeniedCtx.SetParamValues("session-dynamic")
	err = controller.BrokerCredentials(dynamicDeniedCtx)
	var deniedHTTPError *echo.HTTPError
	require.ErrorAs(t, err, &deniedHTTPError)
	require.Equal(t, http.StatusForbidden, deniedHTTPError.Code)

	deniedReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-1/github-credentials?repository=acme%2Fsecret", nil)
	deniedReq.Header.Set("Authorization", "Bearer "+lease)
	deniedCtx := e.NewContext(deniedReq, httptest.NewRecorder())
	deniedCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	deniedCtx.SetParamNames("sessionId")
	deniedCtx.SetParamValues("session-1")
	err = controller.BrokerCredentials(deniedCtx)
	require.ErrorAs(t, err, &deniedHTTPError)
	require.Equal(t, http.StatusForbidden, deniedHTTPError.Code)

	broadLease, broadConnectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "session-2", "", "acme", "acme/payments", []string{".*"}, nil)
	require.NoError(t, err)
	require.Equal(t, connection.ID, broadConnectionID)
	require.True(t, matched)
	otherOrgReq := httptest.NewRequest(http.MethodGet, "/internal/sessions/session-2/github-credentials?repository=other%2Fsecret", nil)
	otherOrgReq.Header.Set("Authorization", "Bearer "+broadLease)
	otherOrgCtx := e.NewContext(otherOrgReq, httptest.NewRecorder())
	otherOrgCtx.SetPath("/internal/sessions/:sessionId/github-credentials")
	otherOrgCtx.SetParamNames("sessionId")
	otherOrgCtx.SetParamValues("session-2")
	err = controller.BrokerCredentials(otherOrgCtx)
	require.ErrorAs(t, err, &deniedHTTPError)
	require.Equal(t, http.StatusForbidden, deniedHTTPError.Code)

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

func TestNormalizeRepositoryPatterns(t *testing.T) {
	t.Parallel()

	got, err := normalizeRepositoryPatterns([]string{"acme/exact", " ^acme/service-[a-z]+$ "})
	require.NoError(t, err)
	require.Equal(t, []string{"acme/exact", "^acme/service-[a-z]+$"}, got)
	lease := githubBrokerLease{Repositories: prefixedRepositoryPatterns(got)}
	require.True(t, repositoryAllowedByLease("ACME/service-api", lease))
	require.False(t, repositoryAllowedByLease("acme/service-123", lease))

	for _, rule := range []string{"[", "(invalid"} {
		_, err := normalizeRepositoryPatterns([]string{rule})
		require.Error(t, err, rule)
	}
}

func TestBrokerCredentialsPreferLinkedPersonalConnectionToken(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "", true)
	connection := githubConnection{ID: "personal-connection", Name: "Personal connection", Enabled: true, BaseURL: "https://github.example.com", APIURL: "https://api.github.example.com", Organizations: []string{"acme"}}
	require.NoError(t, controller.saveConnection(context.Background(), connection, "", ""))

	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeRegular, "alice")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "internal:alice")
	require.NoError(t, err)
	linked, err := controller.linkIdentity(context.Background(), githubIdentity{ID: "identity-personal", PrincipalID: principal.ID, ConnectionID: connection.ID, GitHubUserID: 42, Login: "alice"}, "personal-token", nil)
	require.NoError(t, err)
	require.True(t, linked)

	lease, connectionID, matched, err := controller.IssueBrokerLeaseForRepositories(context.Background(), "personal-session", connection.ID, "acme", "acme/repo", nil, user)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, connection.ID, connectionID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/internal/sessions/personal-session/github-credentials", nil)
	req.Header.Set("Authorization", "Bearer "+lease)
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(req, recorder)
	ctx.SetPath("/internal/sessions/:sessionId/github-credentials")
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("personal-session")
	require.NoError(t, controller.BrokerCredentials(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"username":"x-access-token","token":"personal-token"}`, recorder.Body.String())
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

func TestDeletePrincipalDeletesLinkedIdentities(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	controller := NewGitHubConnectionsController(client, "test", "https://service.example.com")
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:public:42")
	require.NoError(t, err)
	linked, err := controller.linkIdentity(context.Background(), githubIdentity{
		ID: "identity-1", PrincipalID: principal.ID, ConnectionID: "public", GitHubUserID: 42, Login: "alice",
	}, "token", nil)
	require.NoError(t, err)
	require.True(t, linked)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/admin/principals/"+principal.ID, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetPath("/admin/principals/:principalId")
	ctx.SetParamNames("principalId")
	ctx.SetParamValues(principal.ID)
	ctx.Set("internal_user", entities.NewUser(entities.UserID(principal.ID), entities.UserTypeGitHub, "alice"))
	require.NoError(t, controller.DeletePrincipal(ctx))
	require.Equal(t, http.StatusNoContent, rec.Code)

	deletedPrincipal, err := controller.loadPrincipal(context.Background(), principal.InternalUserID)
	require.Empty(t, deletedPrincipal.ID)
	require.True(t, apierrors.IsNotFound(err))
	var identity githubIdentity
	resourceVersion, err := controller.loadObject(context.Background(), identitySecretName("public", 42), &identity)
	require.Empty(t, resourceVersion)
	require.True(t, apierrors.IsNotFound(err))
}

func TestDeletePrincipalReturnsNotFound(t *testing.T) {
	t.Parallel()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "https://service.example.com")
	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest(http.MethodDelete, "/admin/principals/missing", nil), httptest.NewRecorder())
	ctx.SetParamNames("principalId")
	ctx.SetParamValues("missing")
	err := controller.DeletePrincipal(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusNotFound, httpErr.Code)
}

func TestDeletePrincipalWithEncryptedLibSQL(t *testing.T) {
	t.Parallel()
	backend, err := kvstore.NewLibSQLStore(context.Background(), "file://"+filepath.Join(t.TempDir(), "principals.db"), "")
	require.NoError(t, err)
	key := make([]byte, 32)
	bytesRead, err := rand.Read(key)
	require.NoError(t, err)
	require.Equal(t, len(key), bytesRead)
	keyring, err := kvstore.NewLocalKeyring("current", map[string]string{"current": base64.StdEncoding.EncodeToString(key)})
	require.NoError(t, err)
	store, err := kvstore.NewEncryptedStore(backend, keyring)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	controller := NewGitHubConnectionsController(client, "test", "https://service.example.com", true)
	principal, err := controller.getOrCreatePrincipal(context.Background(), "github-connection:public:42")
	require.NoError(t, err)
	linked, err := controller.linkIdentity(context.Background(), githubIdentity{
		ID: "identity-1", PrincipalID: principal.ID, ConnectionID: "public", GitHubUserID: 42, Login: "alice",
	}, "token", nil)
	require.NoError(t, err)
	require.True(t, linked)

	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest(http.MethodDelete, "/admin/principals/"+principal.ID, nil), httptest.NewRecorder())
	ctx.SetParamNames("principalId")
	ctx.SetParamValues(principal.ID)
	require.NoError(t, controller.DeletePrincipal(ctx))
	deletedPrincipal, err := controller.loadPrincipal(context.Background(), principal.InternalUserID)
	require.Empty(t, deletedPrincipal.ID)
	require.True(t, apierrors.IsNotFound(err))
}
