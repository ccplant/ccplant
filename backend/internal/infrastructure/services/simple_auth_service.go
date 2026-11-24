package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// apiTokenRecord is the in-memory representation of a named API token used
// for fast authentication lookups and immediate revocation.
type apiTokenRecord struct {
	tokenID     string
	userID      entities.UserID
	scope       entities.APITokenScope
	teamID      string
	permissions []entities.Permission
	expiresAt   *time.Time
}

type GitHubConnectionCredentialResolver interface {
	ResolveGitHubConnectionCredential(context.Context, string) (*auth.GitHubConnectionCredential, error)
}

// SimpleAuthService implements AuthService with simple in-memory authentication
type SimpleAuthService struct {
	mu                       sync.RWMutex
	apiKeys                  map[string]*services.APIKey
	users                    map[entities.UserID]*entities.User
	keyToUserID              map[string]entities.UserID
	apiKeyConnections        map[string]apiKeyConnection
	apiTokens                map[string]*apiTokenRecord // keyed by plaintext secret
	apiTokenRepo             repositories.APITokenRepository
	localUserRepo            repositories.LocalUserRepository
	githubProvider           *auth.GitHubAuthProvider
	githubAuthConfig         *config.GitHubAuthConfig
	githubCredentialResolver GitHubConnectionCredentialResolver
	teamConfigRepo           repositories.TeamConfigRepository
	teamDiscovery            []config.TeamDiscoveryRule
	githubMembershipResolver interface {
		ResolveTeamMemberships(context.Context, string) ([]auth.GitHubTeamMembership, bool, error)
	}

	// shadowedSecrets records every plaintext secret that has ever been
	// registered as a named API token (via LoadAPIToken or ReconcileAPITokens).
	// For such a secret the named-token store (apiTokens) is authoritative and
	// the legacy apiKeys map must NEVER be consulted for it. This is what
	// makes revocation of a migrated legacy credential stick: a legacy
	// personal/team secret that has been migrated into a named token lives in
	// BOTH apiKeys (loaded by the legacy bootstrap) and apiTokens (loaded by
	// migration). Deleting the named token only removes it from apiTokens, so
	// without shadowing ValidateAPIKey would fall back to apiKeys and keep
	// accepting the supposedly revoked credential. Because shadowedSecrets is
	// sticky (revoking or reconciling away a token never unshadows its
	// secret), re-loading the legacy credential later cannot revive it either.
	shadowedSecrets map[string]struct{}
}

type apiKeyConnection struct {
	provider     string
	connectionID string
}

func (s *SimpleAuthService) SetGitHubConnectionCredentialResolver(resolver GitHubConnectionCredentialResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubCredentialResolver = resolver
}

// ResolveGitHubConnectionCredential exposes the already-wired verifier to the
// HTTP authentication adapter so downstream session creation receives the raw
// GitHub token and its verified connection ID, never the signed envelope.
func (s *SimpleAuthService) ResolveGitHubConnectionCredential(ctx context.Context, credential string) (*auth.GitHubConnectionCredential, error) {
	s.mu.RLock()
	resolver := s.githubCredentialResolver
	s.mu.RUnlock()
	if resolver == nil {
		return nil, errors.New("GitHub connection authentication is unavailable")
	}
	return resolver.ResolveGitHubConnectionCredential(ctx, credential)
}

func (s *SimpleAuthService) SetGitHubMembershipResolver(resolver interface {
	ResolveTeamMemberships(context.Context, string) ([]auth.GitHubTeamMembership, bool, error)
}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubMembershipResolver = resolver
}

// NewSimpleAuthService creates a new SimpleAuthService
func NewSimpleAuthService() *SimpleAuthService {
	return &SimpleAuthService{
		apiKeys:           make(map[string]*services.APIKey),
		users:             make(map[entities.UserID]*entities.User),
		keyToUserID:       make(map[string]entities.UserID),
		apiKeyConnections: make(map[string]apiKeyConnection),
		apiTokens:         make(map[string]*apiTokenRecord),
		shadowedSecrets:   make(map[string]struct{}),
	}
}

// SetAPIKeyConnection records which external connection created a temporary
// login API key.
func (s *SimpleAuthService) SetAPIKeyConnection(apiKey, provider, connectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.apiKeyConnections == nil {
		s.apiKeyConnections = make(map[string]apiKeyConnection)
	}
	s.apiKeyConnections[apiKey] = apiKeyConnection{provider: provider, connectionID: connectionID}
}

// ResolveAPIKeyConnection returns the external connection that issued a
// temporary login API key, if any.
func (s *SimpleAuthService) ResolveAPIKeyConnection(apiKey string) (string, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connection, ok := s.apiKeyConnections[apiKey]
	return connection.provider, connection.connectionID, ok
}

// markShadowedLocked records a secret as authoritative under the named-token
// store. Once shadowed, the legacy apiKeys entry (if any) is never consulted
// for this secret again. Shadowing is monotonic: nothing unshadows a secret
// for the lifetime of the process, so revoking or reconciling away the named
// token cannot let a leftover/reloaded legacy credential back in. Must be
// called with s.mu held for writing.
func (s *SimpleAuthService) markShadowedLocked(secret string) {
	if secret == "" {
		return
	}
	if s.shadowedSecrets == nil {
		s.shadowedSecrets = make(map[string]struct{})
	}
	s.shadowedSecrets[secret] = struct{}{}
}

// IsShadowedLegacySecret reports whether the given plaintext secret has been
// registered as a named token and therefore must authenticate only through
// the named-token store. Used by tests.
func (s *SimpleAuthService) IsShadowedLegacySecret(secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.shadowedSecrets[secret]
	return ok
}

// SetAPITokenRepository wires the named API token repository so the auth
// service can periodically reconcile its in-memory token map against the
// authoritative Kubernetes Secret store. This is what makes revocation of a
// named token eventually consistent across replicas: when a token is deleted
// on one replica, every other replica drops it from its in-memory map on the
// next reconciliation pass. Without this, a replica would keep accepting a
// deleted named token for the lifetime of its process. Legacy static and
// personal API keys live in the separate apiKeys map and are unaffected.
func (s *SimpleAuthService) SetAPITokenRepository(repo repositories.APITokenRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTokenRepo = repo
}

func (s *SimpleAuthService) SetLocalUserRepository(repo repositories.LocalUserRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localUserRepo = repo
}

// ReconcileAPITokens refreshes the in-memory named-token map from the
// repository. Tokens that no longer exist in the store (deleted on another
// replica) are dropped from memory; tokens created on another replica are
// loaded. It is safe to call concurrently with authentication since it holds
// the write lock only while swapping in the freshly built map. The legacy
// apiKeys map is untouched so static/personal API keys remain compatible.
//
// Revocation of a named token is therefore immediate on the replica that
// performs the delete (RevokeAPIToken) and eventually consistent across other
// replicas (bounded by the reconciliation cadence). Global immediate
// revocation across replicas is intentionally NOT claimed.
func (s *SimpleAuthService) ReconcileAPITokens(ctx context.Context) error {
	s.mu.RLock()
	repo := s.apiTokenRepo
	s.mu.RUnlock()
	if repo == nil {
		return nil
	}
	tokens, err := repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("reconcile api tokens: list: %w", err)
	}
	next := make(map[string]*apiTokenRecord, len(tokens))
	for _, t := range tokens {
		next[t.Secret()] = &apiTokenRecord{
			tokenID:     t.ID(),
			userID:      t.UserID(),
			scope:       t.Scope(),
			teamID:      t.TeamID(),
			permissions: t.Permissions(),
			expiresAt:   t.ExpiresAt(),
		}
	}
	s.mu.Lock()
	s.apiTokens = next
	// Any secret currently present in the store is authoritative under the
	// named-token system; shadow it so the legacy apiKeys map is bypassed for
	// it. This is a union (monotonic): secrets of tokens that have since been
	// deleted are NOT unshadowed, so a stale/reloaded legacy entry cannot
	// revive a revoked migrated credential after reconciliation.
	for secret := range next {
		s.markShadowedLocked(secret)
	}
	s.mu.Unlock()
	return nil
}

// SetGitHubAuthConfig sets the GitHub authentication configuration.
// If a provider has already been set via SetGitHubProvider, it is preserved.
// Otherwise a new GitHubAuthProvider is created from the config.
func (s *SimpleAuthService) SetGitHubAuthConfig(cfg *config.GitHubAuthConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubAuthConfig = cfg
	if cfg != nil && cfg.Enabled && s.githubProvider == nil {
		s.githubProvider = auth.NewGitHubAuthProvider(cfg)
	}
}

// SetGitHubProvider injects a pre-configured GitHubAuthProvider.
// This allows the caller to supply a provider that already has optional
// dependencies (e.g. TeamMappingRepository) wired in.
func (s *SimpleAuthService) SetGitHubProvider(provider *auth.GitHubAuthProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubProvider = provider
}

// SetTeamMembershipResolver wires persistent ccplant teams and discovery
// rules into GitHub authentication.
func (s *SimpleAuthService) SetTeamMembershipResolver(repo repositories.TeamConfigRepository, rules []config.TeamDiscoveryRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamConfigRepo = repo
	s.teamDiscovery = append([]config.TeamDiscoveryRule(nil), rules...)
}

func (s *SimpleAuthService) resolveTeamMemberships(user *entities.User, memberships []entities.GitHubTeamMembership) error {
	s.mu.RLock()
	repo := s.teamConfigRepo
	rules := append([]config.TeamDiscoveryRule(nil), s.teamDiscovery...)
	s.mu.RUnlock()
	if repo == nil {
		return nil
	}
	resolver := NewTeamMembershipResolver(repo, rules)
	teamIDs, resolved, err := resolver.ResolveForPrincipal(context.Background(), memberships, string(user.ID()))
	if err != nil {
		return err
	}
	if resolved {
		user.SetResolvedTeamIDs(teamIDs)
	}
	return nil
}

// ResolveTeamMemberships maps provider memberships to canonical ccplant team
// principals before the user is registered with the authentication service.
func (s *SimpleAuthService) ResolveTeamMemberships(user *entities.User, memberships []entities.GitHubTeamMembership) error {
	return s.resolveTeamMemberships(user, memberships)
}

// AuthenticateUser authenticates a user with the given credentials
func (s *SimpleAuthService) AuthenticateUser(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
	if credentials == nil {
		return nil, errors.New("credentials cannot be nil")
	}

	switch credentials.Type {
	case services.CredentialTypePassword:
		return s.authenticateWithPassword(credentials.Username, credentials.Password)
	case services.CredentialTypeToken:
		return s.authenticateWithToken(credentials.Token)
	case services.CredentialTypeAPIKey:
		return s.authenticateWithAPIKey(credentials.APIKey)
	default:
		return nil, fmt.Errorf("unsupported credential type: %s", credentials.Type)
	}
}

// ValidateAPIKey validates an API key and returns the associated user
func (s *SimpleAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*entities.User, error) {
	s.mu.RLock()

	// If this secret has ever been registered as a named API token (e.g. via
	// migration or explicit creation), the named-token store is authoritative.
	// We must never fall back to the legacy apiKeys map for it: otherwise
	// revoking the migrated named token would leave the matching legacy
	// credential (still present in apiKeys from the legacy bootstrap) valid,
	// silently bypassing the revocation. Shadowing is sticky, so a later
	// legacy reload cannot unshadow it either.
	if _, shadowed := s.shadowedSecrets[apiKey]; shadowed {
		rec, repo, err := s.copyAPITokenLocked(apiKey)
		s.mu.RUnlock()
		if err != nil {
			return nil, err
		}
		return validateAPITokenRecord(ctx, rec, repo)
	}

	// Check if API key exists
	key, exists := s.apiKeys[apiKey]
	if !exists {
		rec, repo, err := s.copyAPITokenLocked(apiKey)
		s.mu.RUnlock()
		if err != nil {
			return nil, err
		}
		return validateAPITokenRecord(ctx, rec, repo)
	}

	// Check if API key is expired
	if key.ExpiresAt != nil {
		expiresAt, err := time.Parse(time.RFC3339, *key.ExpiresAt)
		if err == nil && time.Now().After(expiresAt) {
			s.mu.RUnlock()
			return nil, errors.New("API key expired")
		}
	}

	// Get associated user
	userID := s.keyToUserID[apiKey]
	user, exists := s.users[userID]
	if !exists {
		s.mu.RUnlock()
		return nil, errors.New("user not found")
	}
	s.mu.RUnlock()
	return user, nil
}

func (s *SimpleAuthService) copyAPITokenLocked(secret string) (*apiTokenRecord, repositories.LocalUserRepository, error) {
	rec, ok := s.apiTokens[secret]
	if !ok {
		return nil, nil, errors.New("invalid API key")
	}
	copyRec := *rec
	copyRec.permissions = append([]entities.Permission(nil), rec.permissions...)
	if rec.expiresAt != nil {
		exp := *rec.expiresAt
		copyRec.expiresAt = &exp
	}
	return &copyRec, s.localUserRepo, nil
}
