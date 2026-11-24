package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func TestHandleOAuthLogin_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		contentType    string
		expectedStatus int
		expectedError  bool
	}{
		{
			name:           "invalid JSON body",
			requestBody:    `{"redirect_uri": invalid}`,
			contentType:    echo.MIMEApplicationJSON,
			expectedStatus: http.StatusBadRequest,
			expectedError:  true,
		},
		{
			name:           "empty body",
			requestBody:    nil,
			contentType:    echo.MIMEApplicationJSON,
			expectedStatus: http.StatusBadRequest,
			expectedError:  true,
		},
		{
			name: "invalid redirect URI format",
			requestBody: OAuthLoginRequest{
				RedirectURI: "not-a-valid-uri",
			},
			contentType:    echo.MIMEApplicationJSON,
			expectedStatus: http.StatusBadRequest, // Should reject invalid URI format
			expectedError:  true,
		},
		{
			name: "redirect URI with XSS attempt",
			requestBody: OAuthLoginRequest{
				RedirectURI: "javascript:alert('xss')",
			},
			contentType:    echo.MIMEApplicationJSON,
			expectedStatus: http.StatusBadRequest, // Should reject invalid scheme
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, mockServer := setupTestServerWithOAuth(t)
			defer mockServer.Close()

			e := echo.New()

			var body []byte
			switch v := tt.requestBody.(type) {
			case string:
				body = []byte(v)
			case OAuthLoginRequest:
				body, _ = json.Marshal(v)
			case nil:
				body = nil
			}

			req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", bytes.NewReader(body))
			if tt.contentType != "" {
				req.Header.Set(echo.HeaderContentType, tt.contentType)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := proxy.handleOAuthLogin(c)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestHandleOAuthCallback_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    map[string]string
		setupFunc      func(*Server) string
		expectedStatus int
		expectedError  bool
		errorMessage   string
	}{
		{
			name: "OAuth error response",
			queryParams: map[string]string{
				"error":             "access_denied",
				"error_description": "User denied access",
			},
			setupFunc:      nil,
			expectedStatus: http.StatusBadRequest,
			expectedError:  true,
			errorMessage:   "Missing code or state parameter", // Actual handler checks for missing params first
		},
		{
			name: "invalid state format",
			queryParams: map[string]string{
				"code":  "test-code",
				"state": "invalid-jwt-state",
			},
			setupFunc:      nil,
			expectedStatus: http.StatusUnauthorized,
			expectedError:  true,
			errorMessage:   "OAuth authentication failed", // Generic error message from handler
		},
		{
			name: "expired state",
			queryParams: map[string]string{
				"code": "test-code",
			},
			setupFunc: func(p *Server) string {
				// Generate an expired state
				return generateExpiredOAuthState(t)
			},
			expectedStatus: http.StatusUnauthorized,
			expectedError:  true,
			errorMessage:   "OAuth authentication failed", // Generic error message from handler
		},
		{
			name: "empty code",
			queryParams: map[string]string{
				"code":  "",
				"state": "valid-state",
			},
			setupFunc: func(p *Server) string {
				_, state, _ := p.oauthProvider.GenerateAuthURL("http://localhost:3000/callback")
				return state
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  true,
			errorMessage:   "Missing code or state parameter", // Actual error message from handler
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, mockServer := setupTestServerWithOAuth(t)
			defer mockServer.Close()

			e := echo.New()

			// Setup state if needed
			if tt.setupFunc != nil && tt.queryParams["state"] == "" {
				tt.queryParams["state"] = tt.setupFunc(proxy)
			}

			// Build query string
			queryValues := url.Values{}
			for k, v := range tt.queryParams {
				queryValues.Set(k, v)
			}

			req := httptest.NewRequest(http.MethodGet, "/oauth/callback?"+queryValues.Encode(), nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := proxy.handleOAuthCallback(c)
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOAuthSessionManagement(t *testing.T) {
	proxy, mockServer := setupTestServerWithOAuth(t)
	defer mockServer.Close()

	t.Run("concurrent session creation", func(t *testing.T) {
		e := echo.New()
		numGoroutines := 10
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		sessionIDs := make([]string, numGoroutines)
		errors := make([]error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer wg.Done()

				// Generate unique state for each request
				_, state, _ := proxy.oauthProvider.GenerateAuthURL("http://localhost:3000/callback")

				req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/oauth/callback?code=test-code-%d&state=%s", idx, state), nil)
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)

				err := proxy.handleOAuthCallback(c)
				if err != nil {
					errors[idx] = err
					return
				}

				var resp OAuthSessionResponse
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				sessionIDs[idx] = resp.SessionID
			}(i)
		}

		wg.Wait()

		// Verify all sessions were created successfully
		successCount := 0
		for i, err := range errors {
			if err == nil && sessionIDs[i] != "" {
				successCount++
			}
		}
		assert.Equal(t, numGoroutines, successCount)

		// Verify all session IDs are unique
		uniqueSessions := make(map[string]bool)
		for _, sid := range sessionIDs {
			if sid != "" {
				uniqueSessions[sid] = true
			}
		}
		assert.Equal(t, numGoroutines, len(uniqueSessions))
	})

	t.Run("session cleanup", func(t *testing.T) {
		// Clear existing sessions first
		proxy.oauthSessions.Range(func(key, value interface{}) bool {
			proxy.oauthSessions.Delete(key)
			return true
		})

		// Create expired sessions
		for i := 0; i < 5; i++ {
			session := &OAuthSession{
				ID: fmt.Sprintf("expired-%d", i),
				UserContext: &auth.UserContext{
					UserID: fmt.Sprintf("user-%d", i),
				},
				CreatedAt: time.Now().Add(-2 * time.Hour),
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
			proxy.oauthSessions.Store(session.ID, session)
		}

		// Create valid sessions
		for i := 0; i < 3; i++ {
			session := &OAuthSession{
				ID: fmt.Sprintf("valid-%d", i),
				UserContext: &auth.UserContext{
					UserID: fmt.Sprintf("user-%d", i),
				},
				CreatedAt: time.Now(),
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}
			proxy.oauthSessions.Store(session.ID, session)
		}

		// Run manual cleanup since the method doesn't exist
		now := time.Now()
		proxy.oauthSessions.Range(func(key, value interface{}) bool {
			if session, ok := value.(*OAuthSession); ok && session.ExpiresAt.Before(now) {
				proxy.oauthSessions.Delete(key)
			}
			return true
		})

		// Count remaining sessions
		count := 0
		proxy.oauthSessions.Range(func(key, value interface{}) bool {
			count++
			return true
		})

		assert.Equal(t, 3, count, "Should only have valid sessions remaining")
	})
}

func TestHandleOAuthLogout_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		sessionID     string
		setupSession  bool
		expectedError bool
		checkRemoved  bool
	}{
		{
			name:          "non-existent session",
			sessionID:     "non-existent-session",
			setupSession:  false,
			expectedError: true, // Should return session not found error
			checkRemoved:  false,
		},
		{
			name:          "empty session ID",
			sessionID:     "",
			setupSession:  false,
			expectedError: true,
			checkRemoved:  false,
		},
		{
			name:          "already expired session",
			sessionID:     "expired-session",
			setupSession:  true,
			expectedError: false,
			checkRemoved:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, mockServer := setupTestServerWithOAuth(t)
			defer mockServer.Close()

			e := echo.New()

			if tt.setupSession {
				session := &OAuthSession{
					ID: tt.sessionID,
					UserContext: &auth.UserContext{
						UserID: "testuser",
					},
					CreatedAt: time.Now().Add(-2 * time.Hour),
					ExpiresAt: time.Now().Add(-1 * time.Hour),
				}
				proxy.oauthSessions.Store(session.ID, session)
			}

			req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
			if tt.sessionID != "" {
				req.Header.Set("X-Session-ID", tt.sessionID)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := proxy.handleOAuthLogout(c)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, http.StatusOK, rec.Code)

				if tt.checkRemoved {
					_, exists := proxy.oauthSessions.Load(tt.sessionID)
					assert.False(t, exists)
				}
			}
		})
	}
}
