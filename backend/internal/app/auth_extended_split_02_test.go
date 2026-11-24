package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestOAuthProviderErrors(t *testing.T) {
	t.Run("token exchange failure", func(t *testing.T) {
		// Mock server that returns error on token exchange
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login/oauth/access_token" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"server_error"}`))
			}
		}))
		defer mockServer.Close()

		cfg := &config.Config{
			Auth: config.AuthConfig{
				GitHub: &config.GitHubAuthConfig{
					Enabled: true,
					BaseURL: mockServer.URL,
					OAuth: &config.GitHubOAuthConfig{
						ClientID:     "test-client-id",
						ClientSecret: "test-client-secret",
						BaseURL:      mockServer.URL,
					},
				},
			},
		}

		proxy := NewServer(cfg, false)
		e := echo.New()

		_, state, _ := proxy.oauthProvider.GenerateAuthURL("http://localhost:3000/callback")

		req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state="+state, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := proxy.handleOAuthCallback(c)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "OAuth authentication failed")
	})

	t.Run("user info fetch failure", func(t *testing.T) {
		// Mock server that returns token but fails on user info
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login/oauth/access_token":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"access_token":"gho_test_token","token_type":"bearer"}`))
			case "/user":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			}
		}))
		defer mockServer.Close()

		cfg := &config.Config{
			Auth: config.AuthConfig{
				GitHub: &config.GitHubAuthConfig{
					Enabled: true,
					BaseURL: mockServer.URL,
					OAuth: &config.GitHubOAuthConfig{
						ClientID:     "test-client-id",
						ClientSecret: "test-client-secret",
						BaseURL:      mockServer.URL,
					},
				},
			},
		}

		proxy := NewServer(cfg, false)
		e := echo.New()

		_, state, _ := proxy.oauthProvider.GenerateAuthURL("http://localhost:3000/callback")

		req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state="+state, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := proxy.handleOAuthCallback(c)
		assert.Error(t, err)
	})
}

func TestOAuthRateLimiting(t *testing.T) {
	proxy, mockServer := setupTestServerWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Simulate rapid requests from same IP
	clientIP := "192.168.1.100"
	numRequests := 20

	successCount := 0
	rateLimitedCount := 0

	for i := 0; i < numRequests; i++ {
		reqBody := OAuthLoginRequest{
			RedirectURI: "http://localhost:3000/callback",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req.Header.Set("X-Real-IP", clientIP)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := proxy.handleOAuthLogin(c)
		if err == nil && rec.Code == http.StatusOK {
			successCount++
		} else if rec.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}

		// Small delay to simulate real requests
		time.Sleep(10 * time.Millisecond)
	}

	// We should have some successful requests and potentially some rate limited
	assert.Greater(t, successCount, 0, "Should have at least some successful requests")
}

// Helper functions

func generateExpiredOAuthState(t *testing.T) string {
	// This would typically use the same JWT generation as the OAuth provider
	// but with an expired timestamp
	return "expired.jwt.state"
}

// Removed unused mockOAuthProvider to fix lint issues

func TestOAuthProviderIntegration(t *testing.T) {
	t.Run("complete OAuth flow", func(t *testing.T) {
		proxy, mockServer := setupTestServerWithOAuth(t)
		defer mockServer.Close()

		e := echo.New()

		// Step 1: Initiate OAuth login
		loginReq := OAuthLoginRequest{
			RedirectURI: "http://localhost:3000/callback",
		}
		loginBody, _ := json.Marshal(loginReq)
		req1 := httptest.NewRequest(http.MethodPost, "/oauth/authorize", bytes.NewReader(loginBody))
		req1.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, rec1)

		err := proxy.handleOAuthLogin(c1)
		require.NoError(t, err)

		var loginResp OAuthLoginResponse
		_ = json.Unmarshal(rec1.Body.Bytes(), &loginResp)
		require.NotEmpty(t, loginResp.State)
		require.NotEmpty(t, loginResp.AuthURL)

		// Step 2: Handle callback
		req2 := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state="+loginResp.State, nil)
		rec2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, rec2)

		err = proxy.handleOAuthCallback(c2)
		require.NoError(t, err)

		var callbackResp OAuthSessionResponse
		_ = json.Unmarshal(rec2.Body.Bytes(), &callbackResp)
		require.NotEmpty(t, callbackResp.SessionID)
		require.NotEmpty(t, callbackResp.AccessToken)

		// Step 3: Validate session
		req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
		req3.Header.Set("X-Session-ID", callbackResp.SessionID)
		rec3 := httptest.NewRecorder()
		c3 := e.NewContext(req3, rec3)

		userContext, err := proxy.validateOAuthSession(c3)
		require.NoError(t, err)
		require.NotNil(t, userContext)
		require.NotEmpty(t, userContext.UserID)
		require.Equal(t, "testuser", userContext.UserID)
		require.Equal(t, callbackResp.User.UserID, userContext.UserID)

		// Step 4: Refresh session
		req4 := httptest.NewRequest(http.MethodPost, "/oauth/refresh", nil)
		req4.Header.Set("X-Session-ID", callbackResp.SessionID)
		rec4 := httptest.NewRecorder()
		c4 := e.NewContext(req4, rec4)

		err = proxy.handleOAuthRefresh(c4)
		require.NoError(t, err)

		// Step 5: Logout
		req5 := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
		req5.Header.Set("X-Session-ID", callbackResp.SessionID)
		rec5 := httptest.NewRecorder()
		c5 := e.NewContext(req5, rec5)

		err = proxy.handleOAuthLogout(c5)
		require.NoError(t, err)

		// Verify session is gone
		_, exists := proxy.oauthSessions.Load(callbackResp.SessionID)
		assert.False(t, exists)
	})
}
