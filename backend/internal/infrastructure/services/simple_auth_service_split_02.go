package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type githubMembershipResolver interface {
	ResolveTeamMemberships(context.Context, string) ([]auth.GitHubTeamMembership, bool, error)
}

func validateAPITokenRecord(ctx context.Context, rec *apiTokenRecord, localUsers repositories.LocalUserRepository) (*entities.User, error) {
	if rec.expiresAt != nil && time.Now().After(*rec.expiresAt) {
		return nil, errors.New("API key expired")
	}
	if rec.scope == entities.APITokenScopeTeam {
		return entities.NewServiceAccountUser(rec.userID, rec.teamID, rec.permissions), nil
	}
	if localUsers != nil {
		localUser, err := localUsers.GetByID(ctx, rec.userID)
		if err == nil {
			return localUser.ToUser(rec.permissions), nil
		}
		if !errors.Is(err, entities.ErrLocalUserNotFound) {
			return nil, errors.New("user repository unavailable")
		}
	}
	user := entities.NewUser(rec.userID, entities.UserTypeRegular, string(rec.userID))
	user.SetPermissions(rec.permissions)
	return user, nil
}

// LoadAPIToken registers a named API token into the in-memory auth map so it
// is immediately authenticatable. It is safe to call repeatedly for the same
// secret (the latest token metadata wins).
func (s *SimpleAuthService) LoadAPIToken(ctx context.Context, token *entities.APIToken) error {
	if token == nil {
		return errors.New("token cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTokens[token.Secret()] = &apiTokenRecord{
		tokenID:     token.ID(),
		userID:      token.UserID(),
		scope:       token.Scope(),
		teamID:      token.TeamID(),
		permissions: token.Permissions(),
		expiresAt:   token.ExpiresAt(),
	}
	// The named-token store is now authoritative for this secret. Shadow it
	// so any identical legacy apiKeys entry (loaded before or after this) is
	// never consulted. This is the crux of revocation safety for migrated
	// legacy credentials.
	s.markShadowedLocked(token.Secret())
	return nil
}

// RevokeAPIToken removes a named API token from the in-memory auth map,
// effecting immediate revocation. It is safe to call for a secret that is not
// present (returns nil). It deliberately does NOT unshadow the secret: the
// named-token store stays authoritative, so the legacy apiKeys entry (if any)
// cannot validate the revoked secret after this.
func (s *SimpleAuthService) RevokeAPIToken(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apiTokens, secret)
}

// IsAPITokenLoaded reports whether the given plaintext secret is currently
// registered as a named API token. Used by tests.
func (s *SimpleAuthService) IsAPITokenLoaded(secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.apiTokens[secret]
	return ok
}

// ValidatePermission checks if a user has a specific permission
func (s *SimpleAuthService) ValidatePermission(ctx context.Context, user *entities.User, permission entities.Permission) error {
	if user == nil {
		return errors.New("user cannot be nil")
	}

	// Check if user has the permission
	for _, userPerm := range user.Permissions() {
		if userPerm == permission {
			return nil
		}
	}

	// Check if user is admin (admins have all permissions)
	if user.IsAdmin() {
		return nil
	}

	return fmt.Errorf("user does not have permission: %s", permission)
}

// GenerateAPIKey generates a new API key for a user
func (s *SimpleAuthService) GenerateAPIKey(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	keyString := hex.EncodeToString(keyBytes)

	// Set expiration (24 hours from now)
	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	// Create API key
	apiKey := &services.APIKey{
		Key:         keyString,
		UserID:      userID,
		Permissions: permissions,
		ExpiresAt:   &expiresAt,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// Store API key
	s.apiKeys[keyString] = apiKey
	s.keyToUserID[keyString] = userID

	return apiKey, nil
}

// RevokeAPIKey revokes an existing API key
func (s *SimpleAuthService) RevokeAPIKey(ctx context.Context, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if API key exists
	if _, exists := s.apiKeys[apiKey]; !exists {
		return errors.New("API key not found")
	}

	// Remove API key
	delete(s.apiKeys, apiKey)
	delete(s.keyToUserID, apiKey)
	delete(s.apiKeyConnections, apiKey)

	return nil
}

// RefreshUserInfo refreshes user information from external sources
func (s *SimpleAuthService) RefreshUserInfo(ctx context.Context, user *entities.User) (*entities.User, error) {
	// In a simple implementation, just return the user as-is
	// In a real implementation, this would fetch updated info from external sources
	return user, nil
}

// AddUser adds a user to the service (for testing/demo purposes)
func (s *SimpleAuthService) AddUser(user *entities.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID()] = user
}

// LoadBootstrapAdmin registers a non-expiring break-glass admin token. It is
// intentionally independent of GitHub/static auth enablement so a fresh
// installation can always reach the admin configuration UI.
func (s *SimpleAuthService) LoadBootstrapAdmin(userID, username, token string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("bootstrap admin user ID is required")
	}
	if strings.TrimSpace(username) == "" {
		return errors.New("bootstrap admin username is required")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("bootstrap admin token is required")
	}

	user := entities.NewUser(entities.UserID(userID), entities.UserTypeAdmin, username)
	if err := user.SetRoles([]entities.Role{entities.RoleAdmin}); err != nil {
		return fmt.Errorf("set bootstrap admin role: %w", err)
	}
	user.SetPermissions([]entities.Permission{entities.PermissionAdmin})

	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID()] = user
	s.apiKeys[token] = &services.APIKey{
		Key:         token,
		UserID:      user.ID(),
		Permissions: []entities.Permission{entities.PermissionAdmin},
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.keyToUserID[token] = user.ID()
	return nil
}

// authenticateWithPassword authenticates using username and password
func (s *SimpleAuthService) authenticateWithPassword(username, password string) (*entities.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple hardcoded authentication for demo
	if username == "admin" && password == "admin" {
		// Create admin user if not exists
		userID := entities.UserID("admin")
		if user, exists := s.users[userID]; exists {
			return user, nil
		}

		// Create new admin user
		adminUser := entities.NewUser(
			userID,
			entities.UserTypeAdmin,
			"admin",
		)

		s.users[userID] = adminUser
		return adminUser, nil
	}

	return nil, errors.New("invalid username or password")
}

// authenticateWithToken authenticates using a token
// authenticateWithToken authenticates using a token
func (s *SimpleAuthService) authenticateWithToken(token string) (*entities.User, error) {
	s.mu.RLock()
	githubProvider := s.githubProvider
	githubConfig := s.githubAuthConfig
	credentialResolver := s.githubCredentialResolver
	s.mu.RUnlock()

	if githubProvider != nil {
		connectionID := ""
		if auth.IsGitHubConnectionCredential(token) {
			if credentialResolver == nil || githubConfig == nil {
				return nil, errors.New("GitHub connection authentication is unavailable")
			}
			resolved, err := credentialResolver.ResolveGitHubConnectionCredential(context.Background(), token)
			if err != nil {
				return nil, fmt.Errorf("resolve GitHub connection credential: %w", err)
			}
			connectionConfig := *githubConfig
			connectionConfig.BaseURL = resolved.APIURL
			connectionConfig.ConnectionID = resolved.ConnectionID
			githubProvider = auth.NewGitHubAuthProvider(&connectionConfig)
			connectionID = resolved.ConnectionID
			token = resolved.AccessToken
		}
		userContext, err := githubProvider.Authenticate(context.Background(), token)
		if err == nil && userContext != nil {
			return s.userFromGitHubContext(userContext, connectionID)
		}
	}
	return s.authenticateSimpleUserToken(token)
}

func (s *SimpleAuthService) userFromGitHubContext(userContext *auth.UserContext, connectionID string) (*entities.User, error) {
	if err := s.resolveGitHubIdentity(userContext, connectionID); err != nil {
		return nil, err
	}
	if userContext.GitHubUser == nil {
		return nil, errors.New("GitHub user info not available")
	}
	githubInfo := entities.NewGitHubUserInfo(
		userContext.GitHubUser.ID,
		userContext.GitHubUser.Login,
		userContext.GitHubUser.Name,
		userContext.GitHubUser.Email,
		"", "", "",
	)
	teams := make([]entities.GitHubTeamMembership, 0, len(userContext.GitHubUser.Teams))
	for _, team := range userContext.GitHubUser.Teams {
		teams = append(teams, entities.GitHubTeamMembership{
			ConnectionID: team.ConnectionID,
			Organization: team.Organization,
			TeamSlug:     team.TeamSlug,
			TeamName:     team.TeamName,
			Role:         team.Role,
		})
	}
	return s.upsertGitHubUser(userContext, githubInfo, teams)
}

func (s *SimpleAuthService) resolveGitHubIdentity(userContext *auth.UserContext, connectionID string) error {
	s.mu.RLock()
	resolver := s.githubMembershipResolver
	s.mu.RUnlock()
	if connectionID != "" {
		return resolveConnectedGitHubIdentity(userContext, connectionID, resolver)
	}
	if identityResolver, ok := resolver.(interface {
		ResolveApplicationUserIDForGitHubUser(context.Context, int64, string) (string, error)
	}); ok && userContext.GitHubUser != nil {
		applicationUserID, err := identityResolver.ResolveApplicationUserIDForGitHubUser(context.Background(), userContext.GitHubUser.ID, userContext.GitHubUser.Login)
		if err != nil {
			return fmt.Errorf("resolve GitHub application user: %w", err)
		}
		userContext.UserID = applicationUserID
	}
	return resolveBuiltInGitHubMemberships(userContext, resolver)
}

func resolveConnectedGitHubIdentity(userContext *auth.UserContext, connectionID string, resolver githubMembershipResolver) error {
	identityResolver, ok := resolver.(interface {
		ResolveApplicationUserForExternalIdentity(context.Context, string, int64) (string, string, error)
	})
	if !ok || userContext.GitHubUser == nil {
		return errors.New("GitHub connection identity resolver is unavailable")
	}
	applicationUserID, principalID, err := identityResolver.ResolveApplicationUserForExternalIdentity(context.Background(), connectionID, userContext.GitHubUser.ID)
	if err != nil {
		return fmt.Errorf("resolve GitHub connection application user: %w", err)
	}
	userContext.UserID = applicationUserID
	linkedTeams, linked, err := resolver.ResolveTeamMemberships(context.Background(), principalID)
	if err != nil {
		return fmt.Errorf("resolve linked GitHub memberships: %w", err)
	}
	if linked {
		userContext.GitHubUser.Teams = linkedTeams
	}
	return nil
}

func resolveBuiltInGitHubMemberships(userContext *auth.UserContext, resolver githubMembershipResolver) error {
	if resolver == nil || userContext.GitHubUser == nil {
		return nil
	}
	var linkedTeams []auth.GitHubTeamMembership
	var linked bool
	var err error
	if builtIn, ok := resolver.(interface {
		ResolveTeamMembershipsForGitHubUser(context.Context, int64) ([]auth.GitHubTeamMembership, bool, error)
	}); ok {
		linkedTeams, linked, err = builtIn.ResolveTeamMembershipsForGitHubUser(context.Background(), userContext.GitHubUser.ID)
	} else {
		linkedTeams, linked, err = resolver.ResolveTeamMemberships(context.Background(), userContext.UserID)
	}
	if err != nil {
		return fmt.Errorf("resolve linked GitHub memberships: %w", err)
	}
	if linked {
		userContext.GitHubUser.Teams = linkedTeams
	}
	return nil
}

func (s *SimpleAuthService) upsertGitHubUser(userContext *auth.UserContext, githubInfo *entities.GitHubUserInfo, teams []entities.GitHubTeamMembership) (*entities.User, error) {
	userID := entities.UserID(userContext.UserID)
	s.mu.RLock()
	user, exists := s.users[userID]
	s.mu.RUnlock()
	if !exists {
		user = entities.NewGitHubUser(userID, userContext.UserID, userContext.GitHubUser.Email, githubInfo)
	}
	user.SetGitHubInfo(githubInfo, teams)
	if userContext.Role != "" {
		if err := user.SetRoles([]entities.Role{entities.Role(userContext.Role)}); err != nil {
			log.Printf("[AUTH] Warning: failed to set role %q from GitHub context for user %s: %v", userContext.Role, userContext.UserID, err)
		}
	}
	for _, permission := range userContext.Permissions {
		user.AddPermission(entities.Permission(permission))
	}
	if err := s.resolveTeamMemberships(user, teams); err != nil {
		return nil, fmt.Errorf("resolve ccplant team memberships: %w", err)
	}
	if !exists {
		s.mu.Lock()
		s.users[userID] = user
		s.mu.Unlock()
	}
	return user, nil
}

func (s *SimpleAuthService) authenticateSimpleUserToken(token string) (*entities.User, error) {
	if !strings.HasPrefix(token, "user_") {
		return nil, errors.New("invalid token")
	}
	userID := entities.UserID(token)
	s.mu.RLock()
	user, exists := s.users[userID]
	s.mu.RUnlock()
	if exists {
		return user, nil
	}
	user = entities.NewUser(userID, entities.UserTypeRegular, string(userID))
	s.mu.Lock()
	s.users[userID] = user
	s.mu.Unlock()
	return user, nil
}

// authenticateWithAPIKey authenticates using an API key
