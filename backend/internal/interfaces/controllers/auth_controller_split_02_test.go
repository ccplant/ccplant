package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
)

func TestAuthController_GitHubLogin_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	ghAuthenticateUC := auth.NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, ghAuthenticateUC, validatePermUC, presenter)

	reqBody := GitHubLoginRequest{
		Token: "gh_test_token",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/auth/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.GitHubLogin(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, presenter.ghAuthResponse)
}

func TestAuthController_GitHubLogin_InvalidRequestBody(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	ghAuthenticateUC := auth.NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, ghAuthenticateUC, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/github", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.GitHubLogin(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid request body", presenter.lastError)
}

func TestAuthController_ValidateAPIKey_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/validate", nil)
	req.Header.Set("Authorization", "Bearer test_api_key")
	w := httptest.NewRecorder()

	controller.ValidateAPIKey(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, presenter.validResponse)
}

func TestAuthController_ValidateAPIKey_MissingAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/validate", nil)
	w := httptest.NewRecorder()

	controller.ValidateAPIKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "API key is required", presenter.lastError)
}

func TestAuthController_Logout(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()

	controller.Logout(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, presenter.logoutCalled)
}

func TestExtractAPIKeyFromHeader(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		expected   string
	}{
		{
			name:       "Bearer token",
			authHeader: "Bearer test_api_key_123",
			expected:   "test_api_key_123",
		},
		{
			name:       "API-Key header",
			authHeader: "API-Key my_api_key",
			expected:   "my_api_key",
		},
		{
			name:       "Raw value",
			authHeader: "raw_key_value",
			expected:   "raw_key_value",
		},
		{
			name:       "Empty header",
			authHeader: "",
			expected:   "",
		},
		{
			name:       "Short Bearer",
			authHeader: "Bearer",
			expected:   "Bearer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			result := extractAPIKeyFromHeader(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewAuthMiddleware(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	assert.NotNil(t, middleware)
}

func TestAuthMiddleware_Authenticate_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid_api_key")
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Authenticate_MissingAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "authentication required", presenter.lastError)
}

func TestAuthMiddleware_Authenticate_InvalidAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		validateAPIKey: func(ctx context.Context, apiKey string) (*entities.User, error) {
			return nil, errors.New("invalid api key")
		},
	}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid_api_key")
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid API key", presenter.lastError)
}
