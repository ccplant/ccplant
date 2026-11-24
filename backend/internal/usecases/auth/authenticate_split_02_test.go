package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func TestAuthenticateUserUseCase_Execute_UnsupportedCredentialType(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type: "unknown",
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unsupported credential type")
}

func TestAuthenticateUserUseCase_Execute_AuthenticationFailed(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		authenticateUser: func(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
			return nil, errors.New("invalid credentials")
		},
	}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type:     services.CredentialTypePassword,
			Username: "testuser",
			Password: "wrongpass",
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestAuthenticateUserUseCase_Execute_InactiveUser(t *testing.T) {
	userRepo := NewMockUserRepository()
	inactiveUser := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	inactiveUser.Deactivate()

	authSvc := &MockAuthService{
		authenticateUser: func(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
			return inactiveUser, nil
		},
	}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type:     services.CredentialTypePassword,
			Username: "testuser",
			Password: "testpass",
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not active")
}

func TestAuthenticateUserUseCase_Execute_ExistingUser(t *testing.T) {
	userRepo := NewMockUserRepository()
	existingUser := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	userRepo.users["user_123"] = existingUser

	authSvc := &MockAuthService{
		authenticateUser: func(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
			return entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser"), nil
		},
	}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type:     services.CredentialTypePassword,
			Username: "testuser",
			Password: "testpass",
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestAuthenticateUserUseCase_Execute_GenerateAPIKeyFailed(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		generateAPIKey: func(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error) {
			return nil, errors.New("failed to generate API key")
		},
	}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type:     services.CredentialTypePassword,
			Username: "testuser",
			Password: "testpass",
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to generate API key")
}

// Tests for ValidateAPIKeyUseCase

func TestNewValidateAPIKeyUseCase(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	assert.NotNil(t, uc)
}

func TestValidateAPIKeyUseCase_Execute_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	req := &ValidateAPIKeyRequest{
		APIKey: "valid_api_key",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.NotNil(t, resp.User)
}

func TestValidateAPIKeyUseCase_Execute_NilRequest(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	ctx := context.Background()
	resp, err := uc.Execute(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestValidateAPIKeyUseCase_Execute_EmptyAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	req := &ValidateAPIKeyRequest{
		APIKey: "",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "API key cannot be empty")
}

func TestValidateAPIKeyUseCase_Execute_InvalidAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		validateAPIKey: func(ctx context.Context, apiKey string) (*entities.User, error) {
			return nil, errors.New("invalid API key")
		},
	}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	req := &ValidateAPIKeyRequest{
		APIKey: "invalid_api_key",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err) // Should not return error, just mark as invalid
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestValidateAPIKeyUseCase_Execute_InactiveUser(t *testing.T) {
	userRepo := NewMockUserRepository()
	inactiveUser := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	inactiveUser.Deactivate()

	authSvc := &MockAuthService{
		validateAPIKey: func(ctx context.Context, apiKey string) (*entities.User, error) {
			return inactiveUser, nil
		},
	}

	uc := NewValidateAPIKeyUseCase(userRepo, authSvc)

	req := &ValidateAPIKeyRequest{
		APIKey: "valid_api_key",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

// Tests for GitHubAuthenticateUseCase

func TestNewGitHubAuthenticateUseCase(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	assert.NotNil(t, uc)
}

func TestGitHubAuthenticateUseCase_Execute_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	req := &GitHubAuthenticateRequest{
		Token: "gh_valid_token",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.User)
	assert.NotNil(t, resp.APIKey)
	assert.NotNil(t, resp.GitHubInfo)
}

func TestGitHubAuthenticateUseCase_Execute_NilRequest(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	ctx := context.Background()
	resp, err := uc.Execute(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestGitHubAuthenticateUseCase_Execute_EmptyToken(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	req := &GitHubAuthenticateRequest{
		Token: "",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "GitHub token cannot be empty")
}

func TestGitHubAuthenticateUseCase_Execute_AuthenticationFailed(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{
		authenticateWithToken: func(ctx context.Context, token string) (*entities.User, error) {
			return nil, errors.New("GitHub authentication failed")
		},
	}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	req := &GitHubAuthenticateRequest{
		Token: "invalid_token",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "GitHub authentication failed")
}

func TestGitHubAuthenticateUseCase_Execute_GetUserInfoFailed(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{
		getUserInfo: func(ctx context.Context, token string) (*entities.GitHubUserInfo, error) {
			return nil, errors.New("failed to get GitHub user info")
		},
	}

	uc := NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)

	req := &GitHubAuthenticateRequest{
		Token: "valid_token",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "failed to get GitHub user info")
}
