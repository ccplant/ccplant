package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// MockUserRepository implements UserRepository for testing
type MockUserRepository struct {
	users     map[entities.UserID]*entities.User
	saveErr   error
	updateErr error
	findErr   error
}

func NewMockUserRepository() *MockUserRepository {
	return &MockUserRepository{
		users: make(map[entities.UserID]*entities.User),
	}
}

func (m *MockUserRepository) FindByID(ctx context.Context, id entities.UserID) (*entities.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if user, ok := m.users[id]; ok {
		return user, nil
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) Save(ctx context.Context, user *entities.User) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.users[user.ID()] = user
	return nil
}

func (m *MockUserRepository) Update(ctx context.Context, user *entities.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.users[user.ID()] = user
	return nil
}

func (m *MockUserRepository) Delete(ctx context.Context, id entities.UserID) error {
	delete(m.users, id)
	return nil
}

func (m *MockUserRepository) FindByUsername(ctx context.Context, username string) (*entities.User, error) {
	for _, user := range m.users {
		if user.Username() == username {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindAll(ctx context.Context) ([]*entities.User, error) {
	users := make([]*entities.User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	return users, nil
}

func (m *MockUserRepository) FindByEmail(ctx context.Context, email string) (*entities.User, error) {
	for _, user := range m.users {
		if user.Email() != nil && *user.Email() == email {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindByGitHubID(ctx context.Context, githubID int) (*entities.User, error) {
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindByStatus(ctx context.Context, status entities.UserStatus) ([]*entities.User, error) {
	var result []*entities.User
	for _, user := range m.users {
		if user.Status() == status {
			result = append(result, user)
		}
	}
	return result, nil
}

func (m *MockUserRepository) FindByType(ctx context.Context, userType entities.UserType) ([]*entities.User, error) {
	var result []*entities.User
	for _, user := range m.users {
		if user.Type() == userType {
			result = append(result, user)
		}
	}
	return result, nil
}

func (m *MockUserRepository) CountByStatus(ctx context.Context, status entities.UserStatus) (int, error) {
	count := 0
	for _, user := range m.users {
		if user.Status() == status {
			count++
		}
	}
	return count, nil
}

func (m *MockUserRepository) Exists(ctx context.Context, id entities.UserID) (bool, error) {
	_, ok := m.users[id]
	return ok, nil
}

func (m *MockUserRepository) Count(ctx context.Context) (int, error) {
	return len(m.users), nil
}

func (m *MockUserRepository) FindWithFilters(ctx context.Context, filters repositories.UserFilters) ([]*entities.User, error) {
	return m.FindAll(ctx)
}

// MockAuthService implements AuthService for testing
type MockAuthService struct {
	authenticateUser func(ctx context.Context, credentials *services.Credentials) (*entities.User, error)
	validateAPIKey   func(ctx context.Context, apiKey string) (*entities.User, error)
	generateAPIKey   func(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error)
	validatePerm     func(ctx context.Context, user *entities.User, permission entities.Permission) error
}

func (m *MockAuthService) AuthenticateUser(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
	if m.authenticateUser != nil {
		return m.authenticateUser(ctx, credentials)
	}
	return entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser"), nil
}

func (m *MockAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*entities.User, error) {
	if m.validateAPIKey != nil {
		return m.validateAPIKey(ctx, apiKey)
	}
	return entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser"), nil
}

func (m *MockAuthService) ValidatePermission(ctx context.Context, user *entities.User, permission entities.Permission) error {
	if m.validatePerm != nil {
		return m.validatePerm(ctx, user, permission)
	}
	return nil
}

func (m *MockAuthService) GenerateAPIKey(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error) {
	if m.generateAPIKey != nil {
		return m.generateAPIKey(ctx, userID, permissions)
	}
	return &services.APIKey{
		Key:         "test_api_key",
		UserID:      userID,
		Permissions: permissions,
	}, nil
}

func (m *MockAuthService) RevokeAPIKey(ctx context.Context, apiKey string) error {
	return nil
}

func (m *MockAuthService) RefreshUserInfo(ctx context.Context, user *entities.User) (*entities.User, error) {
	return user, nil
}

// MockGitHubAuthService implements GitHubAuthService for testing
type MockGitHubAuthService struct {
	authenticateWithToken func(ctx context.Context, token string) (*entities.User, error)
	getUserInfo           func(ctx context.Context, token string) (*entities.GitHubUserInfo, error)
	getUserTeams          func(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error)
}

func (m *MockGitHubAuthService) AuthenticateWithToken(ctx context.Context, token string) (*entities.User, error) {
	if m.authenticateWithToken != nil {
		return m.authenticateWithToken(ctx, token)
	}
	return entities.NewUser("gh_user_123", entities.UserTypeGitHub, "ghuser"), nil
}

func (m *MockGitHubAuthService) GetUserInfo(ctx context.Context, token string) (*entities.GitHubUserInfo, error) {
	if m.getUserInfo != nil {
		return m.getUserInfo(ctx, token)
	}
	return entities.NewGitHubUserInfo(123, "ghuser", "GitHub User", "gh@example.com", "https://avatar.example.com", "Example Corp", "Tokyo"), nil
}

func (m *MockGitHubAuthService) GetUserTeams(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error) {
	if m.getUserTeams != nil {
		return m.getUserTeams(ctx, token, user)
	}
	return []entities.GitHubTeamMembership{}, nil
}

func (m *MockGitHubAuthService) ValidateGitHubToken(ctx context.Context, token string) (bool, error) {
	return true, nil
}

func (m *MockGitHubAuthService) GenerateOAuthURL(ctx context.Context, redirectURI string) (string, string, error) {
	return "https://github.com/oauth", "state123", nil
}

func (m *MockGitHubAuthService) ExchangeCodeForToken(ctx context.Context, code, state string) (*services.OAuthToken, error) {
	return &services.OAuthToken{
		AccessToken: "test_token",
		TokenType:   "Bearer",
	}, nil
}

func (m *MockGitHubAuthService) RevokeOAuthToken(ctx context.Context, token string) error {
	return nil
}

// Tests for AuthenticateUserUseCase

func TestNewAuthenticateUserUseCase(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	assert.NotNil(t, uc)
}

func TestAuthenticateUserUseCase_Execute_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

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
	assert.NotNil(t, resp.User)
	assert.NotNil(t, resp.APIKey)
	assert.Equal(t, "test_api_key", resp.APIKey.Key)
}

func TestAuthenticateUserUseCase_Execute_NilRequest(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	ctx := context.Background()
	resp, err := uc.Execute(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestAuthenticateUserUseCase_Execute_NilCredentials(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: nil,
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "credentials cannot be nil")
}

func TestAuthenticateUserUseCase_Execute_MissingUsernamePassword(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type: services.CredentialTypePassword,
			// Missing username and password
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "username and password are required")
}

func TestAuthenticateUserUseCase_Execute_MissingToken(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type: services.CredentialTypeToken,
			// Missing token
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "token is required")
}

func TestAuthenticateUserUseCase_Execute_MissingAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	uc := NewAuthenticateUserUseCase(userRepo, authSvc)

	req := &AuthenticateUserRequest{
		Credentials: &services.Credentials{
			Type: services.CredentialTypeAPIKey,
			// Missing API key
		},
	}

	ctx := context.Background()
	resp, err := uc.Execute(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "API key is required")
}
