package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"golang.org/x/sync/singleflight"
)

// hashToken creates a hash of the token for use as cache key
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// GitHubUserInfo represents GitHub user information
type GitHubUserInfo struct {
	Login         string                 `json:"login"`
	ID            int64                  `json:"id"`
	Email         string                 `json:"email"`
	Name          string                 `json:"name"`
	Organizations []GitHubOrganization   `json:"organizations"`
	Teams         []GitHubTeamMembership `json:"teams"`
}

// GitHubOrganization represents GitHub organization information
type GitHubOrganization struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// GitHubTeamMembership represents GitHub team membership
type GitHubTeamMembership struct {
	ConnectionID string `json:"connection_id,omitempty"`
	Organization string `json:"organization"`
	TeamSlug     string `json:"team_slug"`
	TeamName     string `json:"team_name"`
	Role         string `json:"role"`
}

// UserCache represents cached user information, role and permissions
type UserCache struct {
	User        *GitHubUserInfo
	Role        string
	Permissions []string
	EnvFile     string
}

// GitHubAuthProvider handles GitHub OAuth authentication
type GitHubAuthProvider struct {
	config          *config.GitHubAuthConfig
	client          *http.Client
	userCache       *utils.TTLCache       // token hash → UserCache, TTL 5m
	teamCache       *utils.TTLCache       // username → []GitHubTeamMembership, TTL 30s
	teamMappingRepo TeamMappingRepository // ConfigMap persistent cache (optional, may be nil)
	authGroup       singleflight.Group    // coalesces concurrent requests for the same token
}

// NewGitHubAuthProvider creates a new GitHub authentication provider
func NewGitHubAuthProvider(cfg *config.GitHubAuthConfig) *GitHubAuthProvider {
	// Use very short cache TTL in tests to reduce race conditions
	// GitHub identity and team membership are consulted on every authenticated
	// API request. A 30 second TTL made an ordinary page load periodically block
	// on GitHub while several independent UI requests queued behind it. Five
	// minutes keeps permission changes reasonably fresh while removing that
	// latency from normal navigation.
	cacheTTL := 5 * time.Minute
	teamCacheTTL := 5 * time.Minute
	if isTestEnvironment() {
		cacheTTL = 1 * time.Millisecond     // Very short TTL for tests
		teamCacheTTL = 1 * time.Millisecond // Very short TTL for tests
	}

	return &GitHubAuthProvider{
		config:    cfg,
		client:    utils.NewDefaultHTTPClient(),
		userCache: utils.NewTTLCache(cacheTTL),
		teamCache: utils.NewTTLCache(teamCacheTTL),
	}
}

// UpdateConfig refreshes authentication and authorization rules without
// recreating the provider or losing its team-mapping repository.
func (p *GitHubAuthProvider) UpdateConfig(cfg *config.GitHubAuthConfig) {
	if cfg == nil {
		return
	}
	if p.config == nil {
		p.config = cfg
		return
	}
	existingOAuth := p.config.OAuth
	*p.config = *cfg
	if existingOAuth != nil && cfg.OAuth != nil {
		*existingOAuth = *cfg.OAuth
		p.config.OAuth = existingOAuth
	}
}

// SetTeamMappingRepo injects a persistent ConfigMap-backed team mapping repository.
// When set, team memberships will be read from and written to the ConfigMap as a
// secondary cache layer (behind the 30-second in-memory teamCache).
func (p *GitHubAuthProvider) SetTeamMappingRepo(repo TeamMappingRepository) {
	p.teamMappingRepo = repo
}

// isTestEnvironment detects if running in test environment
func isTestEnvironment() bool {
	// Check for test environment indicators
	for _, arg := range []string{"-test.v", "-test.run", "-test.timeout"} {
		for _, osArg := range os.Args {
			if strings.Contains(osArg, arg) {
				return true
			}
		}
	}
	return false
}

// Authenticate authenticates a user using GitHub OAuth token
func (p *GitHubAuthProvider) Authenticate(ctx context.Context, token string) (*UserContext, error) {
	// Check if user information is cached (disabled in test environment)
	var cachedUser *UserCache
	if !isTestEnvironment() {
		cacheKey := fmt.Sprintf("user:%s", hashToken(token))
		if cached, found := p.userCache.Get(cacheKey); found {
			cachedUser = cached.(*UserCache)

			// Re-apply environment variables from cached env file if specified
			if cachedUser.EnvFile != "" {
				envVars, err := config.LoadTeamEnvVars(cachedUser.EnvFile)
				if err != nil {
					log.Printf("[AUTH_DEBUG] Warning: Failed to load cached env file %s: %v", cachedUser.EnvFile, err)
				} else {
					// Apply environment variables
					applied := config.ApplyEnvVars(envVars)
					log.Printf("[AUTH_DEBUG] Applied %d environment variables from cached %s", len(applied), cachedUser.EnvFile)
				}
			}

			return &UserContext{
				UserID:      cachedUser.User.Login,
				Role:        cachedUser.Role,
				Permissions: cachedUser.Permissions,
				AuthType:    "github_oauth",
				GitHubUser:  cachedUser.User,
				EnvFile:     cachedUser.EnvFile,
			}, nil
		}
	}

	// A page load issues several authenticated API requests in parallel. On a
	// cold cache they would all perform the same GitHub user and team lookups,
	// multiplying both latency and rate-limit usage. Let one request populate
	// the caches while the others wait for its result.
	result, err, _ := p.authGroup.Do(hashToken(token), func() (interface{}, error) {
		return p.authenticateUncached(ctx, token)
	})
	if err != nil {
		return nil, err
	}
	return result.(*UserContext), nil
}

// CachedTeamMemberships returns memberships previously resolved for a GitHub
// login without requiring a provider token. This allows another linked login
// method to retain the same team scope.
func (p *GitHubAuthProvider) CachedTeamMemberships(ctx context.Context, username string) ([]GitHubTeamMembership, bool, error) {
	if cached, ok := p.teamCache.Get(username); ok {
		teams := cached.([]GitHubTeamMembership)
		return append([]GitHubTeamMembership(nil), teams...), true, nil
	}
	if p.teamMappingRepo == nil {
		return nil, false, nil
	}
	teams, found, err := p.teamMappingRepo.Get(ctx, username)
	if err != nil || !found {
		return nil, found, err
	}
	p.teamCache.Set(username, teams)
	return append([]GitHubTeamMembership(nil), teams...), true, nil
}

func (p *GitHubAuthProvider) authenticateUncached(ctx context.Context, token string) (*UserContext, error) {
	// Another request may have populated the cache while this request waited to
	// enter the singleflight function.
	if !isTestEnvironment() {
		cacheKey := fmt.Sprintf("user:%s", hashToken(token))
		if cached, found := p.userCache.Get(cacheKey); found {
			cachedUser := cached.(*UserCache)
			return &UserContext{
				UserID:      cachedUser.User.Login,
				Role:        cachedUser.Role,
				Permissions: cachedUser.Permissions,
				AuthType:    "github_oauth",
				GitHubUser:  cachedUser.User,
				EnvFile:     cachedUser.EnvFile,
			}, nil
		}
	}

	user, err := p.getUser(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Get user's team memberships - optimized to check only configured teams
	teams, err := p.getUserTeamsOptimized(ctx, token, user.Login)
	if err != nil {
		log.Printf("Warning: Failed to get user teams for %s: %v", user.Login, err)
		teams = []GitHubTeamMembership{}
	}
	connectionID := p.config.ConnectionID
	if connectionID == "" {
		connectionID = "github"
	}
	for i := range teams {
		teams[i].ConnectionID = connectionID
	}

	user.Teams = teams

	// GitHub authentication is fail-closed by default: a valid GitHub token alone
	// is not enough. Unless explicitly allowed, the user must belong to at least
	// one team configured in team_role_mapping. getUserTeamsOptimized only returns
	// memberships that match those configured rules, including wildcard rules.
	if len(teams) == 0 && !p.config.UserMapping.AllowUsersWithoutTeam {
		return nil, fmt.Errorf("user %q does not belong to any configured team", user.Login)
	}

	// Map user permissions based on team memberships
	role, permissions, envFile := p.mapUserPermissions(teams)

	// Load environment variables from team-specific env file if specified
	if envFile != "" {
		envVars, err := config.LoadTeamEnvVars(envFile)
		if err != nil {
			log.Printf("[AUTH_DEBUG] Warning: Failed to load env file %s: %v", envFile, err)
		} else {
			// Apply environment variables
			applied := config.ApplyEnvVars(envVars)
			log.Printf("[AUTH_DEBUG] Applied %d environment variables from %s", len(applied), envFile)
		}
	}

	// Cache the complete user information (disabled in test environment)
	if !isTestEnvironment() {
		cacheKey := fmt.Sprintf("user:%s", hashToken(token))
		p.userCache.Set(cacheKey, &UserCache{
			User:        user,
			Role:        role,
			Permissions: permissions,
			EnvFile:     envFile,
		})
		log.Printf("[AUTH_DEBUG] Cached user info for %s: role=%s, permissions=%v", user.Login, role, permissions)
	}

	return &UserContext{
		UserID:      user.Login,
		Role:        role,
		Permissions: permissions,
		AuthType:    "github_oauth",
		GitHubUser:  user,
		EnvFile:     envFile,
	}, nil
}

// getUser retrieves user information from GitHub API without caching
func (p *GitHubAuthProvider) getUser(ctx context.Context, token string) (*GitHubUserInfo, error) {
	url := fmt.Sprintf("%s/user", strings.TrimSuffix(p.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	logVerbose("Making GitHub API request: GET %s", url)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("GitHub API request failed: %v", err)
		return nil, err
	}
	defer utils.SafeCloseResponse(resp)

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		return nil, err
	}

	var user GitHubUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// getUserTeamsOptimized retrieves only configured team memberships from GitHub API.
// It checks two cache layers before hitting the GitHub API:
//  1. 30-second in-memory TTL cache (keyed by username)
//  2. ConfigMap persistent cache (keyed by username) — populated across pod restarts
//
// On a cache miss, it fetches from GitHub and updates both cache layers.
func (p *GitHubAuthProvider) getUserTeamsOptimized(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// Layer 1: 30-second in-memory cache
	if cached, ok := p.teamCache.Get(username); ok {
		teams := cached.([]GitHubTeamMembership)
		if len(teams) > 0 {
			log.Printf("[TEAM_CACHE] in-memory cache hit for user %s", username)
			return teams, nil
		}
		log.Printf("[TEAM_CACHE] ignoring empty in-memory cache for user %s", username)
	}

	// Layer 2: ConfigMap persistent cache
	if p.teamMappingRepo != nil {
		if teams, found, err := p.teamMappingRepo.Get(ctx, username); err != nil {
			log.Printf("[TEAM_CACHE] Warning: failed to read ConfigMap cache for user %s: %v", username, err)
		} else if found && len(teams) > 0 {
			log.Printf("[TEAM_CACHE] ConfigMap cache hit for user %s (%d teams)", username, len(teams))
			// Warm up the in-memory cache so subsequent requests within 30s are free
			p.teamCache.Set(username, teams)
			return teams, nil
		}
	}

	// Layer 3: GitHub API
	var (
		teams []GitHubTeamMembership
		err   error
	)
	if p.hasWildcardPatterns() {
		teams, err = p.getUserTeamsWithWildcard(ctx, token)
		if err == nil {
			var exactTeams []GitHubTeamMembership
			exactTeams, err = p.getUserTeamsExactMatch(ctx, token, username)
			teams = mergeTeamMemberships(teams, exactTeams)
		}
	} else {
		teams, err = p.getUserTeamsExactMatch(ctx, token, username)
	}
	if err != nil {
		return nil, err
	}

	// Populate both cache layers
	p.teamCache.Set(username, teams)
	if p.teamMappingRepo != nil {
		if setErr := p.teamMappingRepo.Set(ctx, username, teams); setErr != nil {
			log.Printf("[TEAM_CACHE] Warning: failed to write ConfigMap cache for user %s: %v", username, setErr)
		} else {
			log.Printf("[TEAM_CACHE] ConfigMap cache written for user %s (%d teams)", username, len(teams))
		}
	}

	return teams, nil
}

// getUserTeamsWithWildcard retrieves user teams using /user/teams API and filters by configured patterns
