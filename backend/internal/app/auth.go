package app

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// OAuthLoginRequest represents the request body for OAuth login
type OAuthLoginRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

// OAuthLoginResponse represents the response for OAuth login
type OAuthLoginResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// OAuthCallbackRequest represents the OAuth callback parameters
type OAuthCallbackRequest struct {
	Code  string `query:"code"`
	State string `query:"state"`
}

// OAuthTokenResponse represents the response after successful OAuth
type OAuthTokenResponse struct {
	AccessToken string            `json:"access_token"`
	TokenType   string            `json:"token_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	User        *auth.UserContext `json:"user"`
}

// OAuthSessionResponse represents the response with session information
type OAuthSessionResponse struct {
	SessionID   string            `json:"session_id"`
	AccessToken string            `json:"access_token"`
	TokenType   string            `json:"token_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	User        *auth.UserContext `json:"user"`
}

// OAuthSession represents an authenticated OAuth session
type OAuthSession struct {
	ID          string
	UserContext *auth.UserContext
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// setupAuthRoutes registers authentication-related routes
func (s *Server) setupAuthRoutes() {
	// Add authentication info routes
	authInfoController := controllers.NewAuthInfoController(s.config)
	s.echo.GET("/auth/status", authInfoController.GetAuthStatus)

	// Add OAuth routes if OAuth is configured
	log.Printf("[ROUTES] OAuth provider configured: %v", s.oauthProvider != nil)
	if s.oauthProvider != nil {
		log.Printf("[ROUTES] Registering OAuth endpoints...")
		// OAuth endpoints don't require existing authentication
		s.echo.POST("/oauth/authorize", s.handleOAuthLogin)
		s.echo.GET("/oauth/callback", s.handleOAuthCallback)
		s.echo.POST("/oauth/logout", s.handleOAuthLogout)
		s.echo.POST("/oauth/refresh", s.handleOAuthRefresh)
		log.Printf("[ROUTES] OAuth endpoints registered: /oauth/authorize, /oauth/callback, /oauth/logout, /oauth/refresh")
	} else {
		log.Printf("[ROUTES] OAuth endpoints not registered - OAuth provider not configured")
	}
	if s.router != nil && s.router.handlers.githubConnectionsController != nil {
		s.echo.GET("/auth/github-connections/callback", s.handleGitHubConnectionOAuthCallback)
	}
	if s.router != nil && s.router.handlers.googleConnectionsController != nil {
		s.echo.GET("/auth/google-connections/callback", s.handleGoogleConnectionOAuthCallback)
	}
}

func (s *Server) handleGoogleConnectionOAuthCallback(c echo.Context) error {
	controller := s.router.handlers.googleConnectionsController
	mode, err := controller.OAuthStateMode(c.Request().Context(), c.QueryParam("state"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if mode != "login" {
		return controller.Callback(c)
	}
	result, err := controller.CompleteLogin(c.Request().Context(), c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		return err
	}
	simpleAuth, ok := s.container.AuthService.(*services.SimpleAuthService)
	if !ok {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Google login session authentication is unavailable")
	}
	user := entities.NewUser(entities.UserID(result.UserID), entities.UserTypeRegular, result.Email)
	user.SetPermissions([]entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead, entities.PermissionSessionUpdate, entities.PermissionSessionDelete})
	memberships, _, membershipErr := s.router.handlers.githubConnectionsController.ResolveTeamMemberships(c.Request().Context(), result.PrincipalID)
	if membershipErr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "linked GitHub team memberships could not be resolved").SetInternal(membershipErr)
	}
	if s.oauthProvider != nil {
		cached, found, cacheErr := s.oauthProvider.CachedTeamMemberships(c.Request().Context(), result.UserID)
		if cacheErr != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "cached GitHub team memberships could not be resolved").SetInternal(cacheErr)
		}
		if found {
			memberships = mergeGitHubMemberships(memberships, cached)
		}
	}
	entityMemberships := make([]entities.GitHubTeamMembership, 0, len(memberships))
	for _, membership := range memberships {
		entityMemberships = append(entityMemberships, entities.GitHubTeamMembership{
			ConnectionID: membership.ConnectionID,
			Organization: membership.Organization,
			TeamSlug:     membership.TeamSlug,
			TeamName:     membership.TeamName,
			Role:         membership.Role,
		})
	}
	user.SetGitHubInfo(entities.NewGitHubUserInfo(0, result.UserID, result.Name, result.Email, result.AvatarURL, "", ""), entityMemberships)
	if err := simpleAuth.ResolveTeamMemberships(user, entityMemberships); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "ccplant team memberships could not be resolved").SetInternal(err)
	}
	simpleAuth.AddUser(user)
	apiKey, err := simpleAuth.GenerateAPIKey(c.Request().Context(), user.ID(), user.Permissions())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create application session").SetInternal(err)
	}
	userContext := &auth.UserContext{UserID: result.UserID, AuthType: "google_oidc", AccessToken: apiKey.Key}
	sessionID := uuid.NewString()
	expiresAt := time.Now().Add(24 * time.Hour)
	s.oauthSessions.Store(sessionID, &OAuthSession{ID: sessionID, UserContext: userContext, CreatedAt: time.Now(), ExpiresAt: expiresAt})
	return c.JSON(http.StatusOK, OAuthSessionResponse{SessionID: sessionID, AccessToken: apiKey.Key, TokenType: "Bearer", ExpiresAt: expiresAt, User: userContext})
}

func mergeGitHubMemberships(groups ...[]auth.GitHubTeamMembership) []auth.GitHubTeamMembership {
	merged := make([]auth.GitHubTeamMembership, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, membership := range group {
			key := strings.ToLower(strings.TrimSpace(membership.Organization)) + "\x00" + strings.ToLower(strings.TrimSpace(membership.TeamSlug))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, membership)
		}
	}
	return merged
}

func (s *Server) handleGitHubConnectionOAuthCallback(c echo.Context) error {
	controller := s.router.handlers.githubConnectionsController
	mode, err := controller.OAuthStateMode(c.Request().Context(), c.QueryParam("state"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if mode != "login" {
		return controller.Callback(c)
	}
	result, err := controller.CompleteLogin(c.Request().Context(), c.QueryParam("state"), c.QueryParam("code"))
	if err != nil {
		return err
	}
	if s.config.Auth.GitHub == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication is not configured")
	}
	authConfig := *s.config.Auth.GitHub
	authConfig.BaseURL = result.APIURL
	authConfig.ConnectionID = result.ConnectionID
	provider := auth.NewGitHubAuthProvider(&authConfig)
	userContext, err := provider.Authenticate(c.Request().Context(), result.AccessToken)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed").SetInternal(err)
	}
	userContext.UserID = result.UserID
	userContext.AuthType = "github_oauth"
	userContext.AccessToken = result.AccessToken
	if userContext.GitHubUser != nil {
		memberships, _, resolveErr := controller.ResolveTeamMemberships(c.Request().Context(), result.UserID)
		if resolveErr != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "GitHub team memberships could not be resolved").SetInternal(resolveErr)
		}
		userContext.GitHubUser.Teams = memberships
	}
	sessionID := uuid.NewString()
	expiresAt := time.Now().Add(24 * time.Hour)
	s.oauthSessions.Store(sessionID, &OAuthSession{ID: sessionID, UserContext: userContext, CreatedAt: time.Now(), ExpiresAt: expiresAt})
	return c.JSON(http.StatusOK, OAuthSessionResponse{SessionID: sessionID, AccessToken: result.AccessToken, TokenType: "Bearer", ExpiresAt: expiresAt, User: userContext})
}

// handleOAuthLogin initiates the OAuth flow
func (s *Server) handleOAuthLogin(c echo.Context) error {
	var req OAuthLoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate redirect URI
	if req.RedirectURI == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "redirect_uri is required")
	}

	// Validate redirect URI format
	redirectURL, err := url.Parse(req.RedirectURI)
	if err != nil || redirectURL.Scheme == "" || redirectURL.Host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid redirect_uri format")
	}

	// Validate redirect URI against whitelist
	if !isAllowedRedirectURI(req.RedirectURI) {
		log.Printf("Blocked unauthorized redirect URI: %s", req.RedirectURI)
		return echo.NewHTTPError(http.StatusBadRequest, "Unauthorized redirect_uri")
	}

	// Generate OAuth URL
	authURL, state, err := s.oauthProvider.GenerateAuthURL(req.RedirectURI)
	if err != nil {
		log.Printf("Failed to generate OAuth URL: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate authorization URL")
	}

	return c.JSON(http.StatusOK, OAuthLoginResponse{
		AuthURL: authURL,
		State:   state,
	})
}

// handleOAuthCallback handles the OAuth callback
func (s *Server) handleOAuthCallback(c echo.Context) error {
	var req OAuthCallbackRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid callback parameters")
	}

	// Validate required parameters
	if req.Code == "" || req.State == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Missing code or state parameter")
	}

	// Exchange code for token
	userContext, err := s.oauthProvider.ExchangeCode(c.Request().Context(), req.Code, req.State)
	if err != nil {
		log.Printf("OAuth code exchange failed: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "OAuth authentication failed")
	}
	if s.router != nil && s.router.handlers.githubConnectionsController != nil &&
		userContext.GitHubUser != nil && userContext.GitHubUser.ID != 0 {
		principalID, err := s.router.handlers.githubConnectionsController.ResolvePrincipalIDForGitHubUser(
			c.Request().Context(), userContext.GitHubUser.ID,
		)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to resolve principal").SetInternal(err)
		}
		userContext.UserID = principalID
		if s.router.handlers.teamMembershipController != nil {
			if syncErr := s.router.handlers.teamMembershipController.AutoSyncForPrincipalWithToken(c.Request().Context(), principalID, "identity_created", userContext.AccessToken); syncErr != nil {
				log.Printf("[GITHUB_MEMBERSHIP] Initial built-in OAuth sync failed for principal %q: %v", principalID, syncErr)
			}
		}
	}

	// Create a new session for the authenticated user
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour) // Token expires in 24 hours

	// Store session information (in production, use a proper session store)
	s.oauthSessions.Store(sessionID, &OAuthSession{
		ID:          sessionID,
		UserContext: userContext,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
	})

	// Return session information
	return c.JSON(http.StatusOK, OAuthSessionResponse{
		SessionID:   sessionID,
		AccessToken: userContext.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        userContext,
	})
}

// handleOAuthLogout handles OAuth logout
func (s *Server) handleOAuthLogout(c echo.Context) error {
	// Get the session ID from the Authorization header or query parameter
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = c.QueryParam("session_id")
	}

	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID required")
	}

	// Get session from store
	sessionValue, ok := s.oauthSessions.Load(sessionID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	session := sessionValue.(*OAuthSession)

	if session.UserContext.AuthType == "google_oidc" {
		if err := s.container.AuthService.RevokeAPIKey(c.Request().Context(), session.UserContext.AccessToken); err != nil {
			log.Printf("Failed to revoke Google application session: %v", err)
		}
	} else if s.oauthProvider != nil {
		// Revoke the GitHub token.
		if err := s.oauthProvider.RevokeToken(c.Request().Context(), session.UserContext.AccessToken); err != nil {
			log.Printf("Failed to revoke GitHub token: %v", err)
		}
	}

	// Remove session from store
	s.oauthSessions.Delete(sessionID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Successfully logged out",
	})
}

// handleOAuthRefresh handles token refresh (if needed in the future)
func (s *Server) handleOAuthRefresh(c echo.Context) error {
	// GitHub OAuth tokens don't expire, so this is a placeholder
	// In a real implementation, you might want to validate the token is still valid
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID required")
	}

	sessionValue, ok := s.oauthSessions.Load(sessionID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	session := sessionValue.(*OAuthSession)

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		s.oauthSessions.Delete(sessionID)
		return echo.NewHTTPError(http.StatusUnauthorized, "Session expired")
	}

	// Extend session expiration
	session.ExpiresAt = time.Now().Add(24 * time.Hour)

	return c.JSON(http.StatusOK, OAuthTokenResponse{
		AccessToken: session.UserContext.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   session.ExpiresAt,
		User:        session.UserContext,
	})
}

// isAllowedRedirectURI validates if the redirect URI is in the allowed list
func isAllowedRedirectURI(redirectURI string) bool {
	// Get allowed redirect URIs from environment variable
	allowedURIs := os.Getenv("OAUTH_ALLOWED_REDIRECT_URIS")
	if allowedURIs == "" {
		// Fallback to localhost for development
		allowedDefaultURIs := []string{
			"http://localhost",
			"https://localhost",
			"http://127.0.0.1",
			"https://127.0.0.1",
		}
		for _, allowed := range allowedDefaultURIs {
			if strings.HasPrefix(redirectURI, allowed) {
				return true
			}
		}
		return false
	}

	// Parse comma-separated allowed URIs
	uris := strings.Split(allowedURIs, ",")
	for _, allowed := range uris {
		allowed = strings.TrimSpace(allowed)
		if allowed == redirectURI {
			return true
		}
		// Also allow prefix match for same domain with different paths
		if strings.HasPrefix(redirectURI, allowed) {
			return true
		}
	}

	return false
}

// validateOAuthSession validates an OAuth session from the request
func (s *Server) validateOAuthSession(c echo.Context) (*auth.UserContext, error) {
	// Try to get session ID from header first, then from query parameter
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = c.QueryParam("session_id")
	}

	if sessionID == "" {
		// Try to extract from Authorization header as Bearer token
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			sessionID = authHeader[7:]
		}
	}

	if sessionID == "" {
		return nil, fmt.Errorf("no session ID provided")
	}

	// Get session from store
	sessionValue, ok := s.oauthSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session := sessionValue.(*OAuthSession)

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		s.oauthSessions.Delete(sessionID)
		return nil, fmt.Errorf("session expired")
	}

	return session.UserContext, nil
}

// cleanupExpiredOAuthSessions periodically cleans up expired OAuth sessions
func (s *Server) cleanupExpiredOAuthSessions() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var toDelete []string

		s.oauthSessions.Range(func(key, value interface{}) bool {
			sessionID := key.(string)
			session := value.(*OAuthSession)

			if now.After(session.ExpiresAt) {
				toDelete = append(toDelete, sessionID)
			}
			return true
		})

		for _, sessionID := range toDelete {
			s.oauthSessions.Delete(sessionID)
			log.Printf("Cleaned up expired OAuth session: %s", sessionID)
		}
	}
}
