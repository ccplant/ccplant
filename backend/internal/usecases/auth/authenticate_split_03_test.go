package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestGitHubAuthenticateUseCase_Execute_InactiveUser(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	inactiveUser := entities.NewUser("gh_user_123", entities.UserTypeGitHub, "ghuser")
	inactiveUser.Deactivate()

	ghAuthSvc := &MockGitHubAuthService{
		authenticateWithToken: func(ctx context.Context, token string) (*entities.User, error) {
			return inactiveUser, nil
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
	assert.Contains(t, err.Error(), "not active")
}

// Tests for ValidatePermissionUseCase

func TestNewValidatePermissionUseCase(t *testing.T) {
	authSvc := &MockAuthService{}

	uc := NewValidatePermissionUseCase(authSvc)

	assert.NotNil(t, uc)
}

func TestValidatePermissionUseCase_Execute_Success(t *testing.T) {
	authSvc := &MockAuthService{}

	uc := NewValidatePermissionUseCase(authSvc)

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	req := &ValidatePermissionRequest{
		User:       user,
		Permission: entities.PermissionSessionRead,
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.HasPermission)
}

func TestValidatePermissionUseCase_Execute_NilRequest(t *testing.T) {
	authSvc := &MockAuthService{}

	uc := NewValidatePermissionUseCase(authSvc)

	ctx := context.Background()
	resp, err := uc.Execute(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestValidatePermissionUseCase_Execute_NilUser(t *testing.T) {
	authSvc := &MockAuthService{}

	uc := NewValidatePermissionUseCase(authSvc)

	req := &ValidatePermissionRequest{
		User:       nil,
		Permission: entities.PermissionSessionRead,
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "user cannot be nil")
}

func TestValidatePermissionUseCase_Execute_EmptyPermission(t *testing.T) {
	authSvc := &MockAuthService{}

	uc := NewValidatePermissionUseCase(authSvc)

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	req := &ValidatePermissionRequest{
		User:       user,
		Permission: "",
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "permission cannot be empty")
}

func TestValidatePermissionUseCase_Execute_PermissionDenied(t *testing.T) {
	authSvc := &MockAuthService{
		validatePerm: func(ctx context.Context, user *entities.User, permission entities.Permission) error {
			return errors.New("permission denied")
		},
	}

	uc := NewValidatePermissionUseCase(authSvc)

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	req := &ValidatePermissionRequest{
		User:       user,
		Permission: entities.PermissionAdmin,
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.NoError(t, err) // Should not return error, just mark as denied
	assert.NotNil(t, resp)
	assert.False(t, resp.HasPermission)
	assert.NotNil(t, resp.Error)
}
