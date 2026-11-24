package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// Integration test for the complete GitHub OAuth flow
func TestGitHubOAuthIntegration(t *testing.T) {
	mockGitHub := httptest.NewServer(githubOAuthTestHandler(t))
	defer mockGitHub.Close()
	authProvider, oauthProvider := githubOAuthTestProviders(mockGitHub.URL)
	t.Run("complete OAuth flow", func(t *testing.T) { testCompleteOAuthFlow(t, oauthProvider) })
	t.Run("handle OAuth errors", func(t *testing.T) { testOAuthErrors(t, authProvider, oauthProvider) })
	t.Run("concurrent OAuth flows", func(t *testing.T) { testConcurrentOAuthFlows(t, oauthProvider) })
}

func githubOAuthTestProviders(baseURL string) (*GitHubAuthProvider, *GitHubOAuthProvider) {
	githubCfg := &config.GitHubAuthConfig{
		Enabled: true,
		BaseURL: baseURL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"test-org/engineering": {
					Role:        "developer",
					Permissions: []string{"read", "write"},
				},
				"test-org/admins": {
					Role:        "admin",
					Permissions: []string{"admin"},
				},
			},
		},
	}

	oauthCfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org project",
		BaseURL:      baseURL,
	}
	authProvider := NewGitHubAuthProvider(githubCfg)
	return authProvider, NewGitHubOAuthProvider(oauthCfg, NewGitHubAuthProvider(githubCfg))
}

func githubOAuthTestHandler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/authorize", oauthAuthorizeHandler(t))
	mux.HandleFunc("/login/oauth/access_token", oauthTokenHandler(t))
	mux.HandleFunc("/user", authenticatedJSONHandler(t, map[string]interface{}{
		"login": "integration-test-user", "id": 999999,
		"email": "integration@test.com", "name": "Integration Test User",
	}))
	mux.HandleFunc("/user/orgs", authenticatedJSONHandler(t, []map[string]interface{}{{"login": "test-org", "id": 12345}}))
	mux.HandleFunc("/user/teams", authenticatedJSONHandler(t, oauthTestTeams()))
	mux.HandleFunc("/orgs/test-org/teams", authenticatedJSONHandler(t, []map[string]interface{}{
		{"slug": "engineering", "name": "Engineering"},
		{"slug": "admins", "name": "Administrators"},
	}))
	mux.HandleFunc("/orgs/test-org/teams/engineering/memberships/integration-test-user", authenticatedJSONHandler(t, map[string]interface{}{"state": "active", "role": "member"}))
	mux.HandleFunc("/orgs/test-org/teams/admins/memberships/integration-test-user", authenticatedJSONHandler(t, map[string]interface{}{"state": "active", "role": "maintainer"}))
	return mux
}

func oauthAuthorizeHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callbackURL, err := url.Parse(r.URL.Query().Get("redirect_uri"))
		require.NoError(t, err)
		query := callbackURL.Query()
		query.Set("code", "test-auth-code-"+r.URL.Query().Get("client_id"))
		query.Set("state", r.URL.Query().Get("state"))
		callbackURL.RawQuery = query.Encode()
		w.Header().Set("Location", callbackURL.String())
		w.WriteHeader(http.StatusFound)
	}
}

func oauthTokenHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		valid := r.Form.Get("client_id") == "test-client-id" &&
			r.Form.Get("client_secret") == "test-client-secret" &&
			r.Form.Get("code") == "test-auth-code-test-client-id"
		if !valid {
			w.WriteHeader(http.StatusUnauthorized)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "gho_integration_test_token", "token_type": "bearer",
			"scope": "read:user,read:org,project",
		}))
	}
}

func authenticatedJSONHandler(t *testing.T, response interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gho_integration_test_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(response))
	}
}

func oauthTestTeams() []map[string]interface{} {
	return []map[string]interface{}{
		{"id": 1, "name": "Engineering", "slug": "engineering", "permission": "push", "organization": map[string]interface{}{"login": "test-org"}},
		{"id": 2, "name": "Administrators", "slug": "admins", "permission": "admin", "organization": map[string]interface{}{"login": "test-org"}},
	}
}

func testCompleteOAuthFlow(t *testing.T, oauthProvider *GitHubOAuthProvider) {
	authURL, state, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
	require.NoError(t, err)
	parsedURL, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.Equal(t, "/login/oauth/authorize", parsedURL.Path)
	assert.Equal(t, "test-client-id", parsedURL.Query().Get("client_id"))
	assert.Equal(t, state, parsedURL.Query().Get("state"))
	userContext, err := oauthProvider.ExchangeCode(context.Background(), "test-auth-code-test-client-id", state)
	require.NoError(t, err)
	assert.Equal(t, "integration-test-user", userContext.UserID)
	assert.Equal(t, "integration@test.com", userContext.GitHubUser.Email)
	assert.Equal(t, "gho_integration_test_token", userContext.AccessToken)
	assert.ElementsMatch(t, []string{"read", "write", "admin"}, userContext.Permissions)
}

func testOAuthErrors(t *testing.T, authProvider *GitHubAuthProvider, oauthProvider *GitHubOAuthProvider) {
	ctx := context.Background()
	_, validState, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
	require.NoError(t, err)
	userContext, err := oauthProvider.ExchangeCode(ctx, "invalid-code", validState)
	assert.Nil(t, userContext)
	require.ErrorContains(t, err, "failed to exchange")
	userContext, err = oauthProvider.ExchangeCode(ctx, "test-auth-code-test-client-id", "invalid-state")
	assert.Nil(t, userContext)
	assert.Error(t, err)
	userContext, err = authProvider.Authenticate(ctx, "invalid-token")
	assert.Nil(t, userContext)
	assert.Error(t, err)
}

func testConcurrentOAuthFlows(t *testing.T, oauthProvider *GitHubOAuthProvider) {
	const numFlows = 3
	results := make(chan error, numFlows)
	for range numFlows {
		go func() {
			_, state, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
			if err == nil {
				var userContext *UserContext
				userContext, err = oauthProvider.ExchangeCode(context.Background(), "test-auth-code-test-client-id", state)
				if err == nil && userContext.UserID != "integration-test-user" {
					err = assert.AnError
				}
			}
			results <- err
		}()
	}
	for range numFlows {
		assert.NoError(t, <-results)
	}
}

// Test OAuth flow with various edge cases
func TestGitHubOAuthEdgeCases(t *testing.T) {
	for _, testCase := range githubOAuthEdgeCases(t) {
		t.Run(testCase.name, func(t *testing.T) { runGitHubOAuthEdgeCase(t, testCase) })
	}
}

type githubOAuthEdgeCase struct {
	name          string
	serverHandler http.HandlerFunc
	expectError   bool
	errorContains string
}

func githubOAuthEdgeCases(t *testing.T) []githubOAuthEdgeCase {
	return []githubOAuthEdgeCase{
		{
			name: "GitHub API timeout",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Simulate timeout by not responding
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusGatewayTimeout)
			},
			expectError:   true,
			errorContains: "504",
		},
		{
			name: "GitHub API rate limit",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", "1234567890")
				w.WriteHeader(http.StatusForbidden)
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
					"message": "API rate limit exceeded",
				}))
			},
			expectError:   true,
			errorContains: "403",
		},
		{
			name: "Invalid JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{invalid json`))
				assert.NoError(t, err)
			},
			expectError:   true,
			errorContains: "invalid character",
		},
		{
			name: "Empty access token response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login/oauth/access_token" {
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
						"access_token": "",
					}))
				}
			},
			expectError:   true,
			errorContains: "no access token",
		},
	}
}

func runGitHubOAuthEdgeCase(t *testing.T, testCase githubOAuthEdgeCase) {
	mockServer := httptest.NewServer(testCase.serverHandler)
	defer mockServer.Close()

	githubCfg := &config.GitHubAuthConfig{Enabled: true, BaseURL: mockServer.URL}
	oauthCfg := &config.GitHubOAuthConfig{
		ClientID: "test-client-id", ClientSecret: "test-client-secret", BaseURL: mockServer.URL,
	}
	oauthProvider := NewGitHubOAuthProvider(oauthCfg, NewGitHubAuthProvider(githubCfg))
	_, state, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
	require.NoError(t, err)
	userContext, err := oauthProvider.ExchangeCode(context.Background(), "test-code", state)

	if testCase.expectError {
		assert.Nil(t, userContext)
		assert.Error(t, err)
		if testCase.errorContains != "" {
			assert.ErrorContains(t, err, testCase.errorContains)
		}
		return
	}
	assert.NoError(t, err)
}

// Benchmark OAuth operations
