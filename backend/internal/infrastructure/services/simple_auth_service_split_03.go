package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func (s *SimpleAuthService) authenticateWithAPIKey(apiKey string) (*entities.User, error) {
	return s.ValidateAPIKey(context.Background(), apiKey)
}

// CreateServiceAccountForTeam creates a service account for a team
func (s *SimpleAuthService) CreateServiceAccountForTeam(ctx context.Context, teamID string, teamConfigRepo repositories.TeamConfigRepository) (*entities.User, *entities.ServiceAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	apiKey := hex.EncodeToString(keyBytes)

	// Generate UserID from team ID
	userID := entities.UserID(fmt.Sprintf("sa-%s", strings.ReplaceAll(teamID, "/", "-")))

	// Default permissions for service accounts
	permissions := []entities.Permission{
		entities.PermissionSessionCreate,
		entities.PermissionSessionRead,
		entities.PermissionSessionUpdate,
		entities.PermissionSessionDelete,
	}

	// Create User entity
	user := entities.NewServiceAccountUser(userID, teamID, permissions)

	// Create ServiceAccount entity
	serviceAccount := entities.NewServiceAccount(teamID, userID, apiKey, permissions)

	// Get or create TeamConfig
	teamConfig, err := teamConfigRepo.FindByTeamID(ctx, teamID)
	if err != nil {
		// TeamConfig doesn't exist, create new one
		teamConfig = entities.NewTeamConfig(teamID, serviceAccount, nil)
	} else {
		// Update existing TeamConfig with service account
		teamConfig.SetServiceAccount(serviceAccount)
	}

	// Save to repository
	if err := teamConfigRepo.Save(ctx, teamConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to save team config: %w", err)
	}

	// Store in memory maps
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      userID,
		Permissions: permissions,
		CreatedAt:   serviceAccount.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Service accounts don't expire
	}
	s.keyToUserID[apiKey] = userID
	s.users[userID] = user

	return user, serviceAccount, nil
}

// LoadServiceAccountFromTeamConfig loads a service account from team config into memory
func (s *SimpleAuthService) LoadServiceAccountFromTeamConfig(ctx context.Context, teamConfig *entities.TeamConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	serviceAccount := teamConfig.ServiceAccount()
	if serviceAccount == nil {
		return errors.New("team config does not have a service account")
	}

	// Create User entity
	user := entities.NewServiceAccountUser(
		serviceAccount.UserID(),
		teamConfig.TeamID(),
		serviceAccount.Permissions(),
	)

	// Store in memory maps
	apiKey := serviceAccount.APIKey()
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      serviceAccount.UserID(),
		Permissions: serviceAccount.Permissions(),
		CreatedAt:   serviceAccount.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Service accounts don't expire
	}
	s.keyToUserID[apiKey] = serviceAccount.UserID()
	s.users[serviceAccount.UserID()] = user

	return nil
}

// LoadPersonalAPIKey loads a personal API key into memory
func (s *SimpleAuthService) LoadPersonalAPIKey(ctx context.Context, personalAPIKey *entities.PersonalAPIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID := personalAPIKey.UserID()
	apiKey := personalAPIKey.APIKey()

	// Check if user already exists
	user, exists := s.users[userID]
	if !exists {
		// Create a new user entity for this personal API key
		// Default to regular user type with basic permissions
		user = entities.NewUser(
			userID,
			entities.UserTypeRegular,
			string(userID),
		)
		// Add default permissions
		user.AddPermission(entities.PermissionSessionCreate)
		user.AddPermission(entities.PermissionSessionRead)
		user.AddPermission(entities.PermissionSessionUpdate)
		user.AddPermission(entities.PermissionSessionDelete)

		s.users[userID] = user
	}

	// Store API key in memory maps
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      userID,
		Permissions: user.Permissions(),
		CreatedAt:   personalAPIKey.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Personal API keys don't expire
	}
	s.keyToUserID[apiKey] = userID

	return nil
}
