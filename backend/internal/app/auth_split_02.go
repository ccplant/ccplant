package app

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

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
		ExpiresAt: session.ExpiresAt,
		User:      publicOAuthUser(session.UserContext),
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
			if sameRedirectOrigin(redirectURI, allowed) {
				return true
			}
		}
		return false
	}

	// Parse comma-separated allowed URIs
	uris := strings.Split(allowedURIs, ",")
	for _, allowed := range uris {
		allowed = strings.TrimSpace(allowed)
		if sameRedirectOrigin(redirectURI, allowed) {
			return true
		}
	}

	return false
}

func sameRedirectOrigin(rawRedirect, rawAllowed string) bool {
	redirect, err := url.Parse(rawRedirect)
	if err != nil || redirect.Scheme == "" || redirect.Host == "" || redirect.User != nil {
		return false
	}
	allowed, err := url.Parse(rawAllowed)
	if err != nil || allowed.Scheme == "" || allowed.Host == "" || allowed.User != nil {
		return false
	}
	if !strings.EqualFold(redirect.Scheme, allowed.Scheme) || !strings.EqualFold(redirect.Hostname(), allowed.Hostname()) {
		return false
	}
	if allowed.Port() != "" && redirect.Port() != allowed.Port() {
		return false
	}
	allowedPath := strings.TrimSuffix(allowed.EscapedPath(), "/")
	redirectPath := strings.TrimSuffix(redirect.EscapedPath(), "/")
	return allowedPath == "" || redirectPath == allowedPath || strings.HasPrefix(redirectPath, allowedPath+"/")
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
