package auth

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

func UserOwnsSession(c echo.Context, sessionUserID string) bool {
	user := GetUserFromContext(c)
	if user == nil {
		log.Printf("Session access denied: no authenticated user for session %s from %s", sessionUserID, c.RealIP())
		return false
	}

	// Use internal user's permission checking
	sessionUserEntityID := entities.UserID(sessionUserID)
	canAccess := user.CanAccessSession(sessionUserEntityID)

	if !canAccess {
		log.Printf("Session access denied: user %s (type: %s) attempted to access session owned by %s from %s",
			user.ID(), user.UserType(), sessionUserID, c.RealIP())
	}

	return canAccess
}

// tryInternalAPIKeyAuth attempts API key authentication using internal auth service
func tryInternalAPIKeyAuth(c echo.Context, cfg *config.Config, authService services.AuthService) (*entities.User, error) {
	var apiKey string

	// Try the X-API-Key header (used by the AGENTAPI_AUTH_ADMIN_KEY and named API tokens)
	apiKey = c.Request().Header.Get("X-API-Key")

	// If not found in the API key header, try to extract from Authorization header (Bearer token)
	if apiKey == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader != "" {
			apiKey = extractAPIKeyFromAuthHeader(authHeader)
		}
	}

	if apiKey == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "API key required")
	}

	// Use internal auth service to validate API key
	user, err := authService.ValidateAPIKey(c.Request().Context(), apiKey)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
	}

	return user, nil
}

// tryInternalGitHubAuth attempts GitHub authentication using internal auth service
func tryInternalGitHubAuth(c echo.Context, cfg *config.Config, authService services.AuthService) (*entities.User, error) {
	if cfg.Auth.GitHub == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub auth not configured")
	}

	tokenHeader := c.Request().Header.Get(cfg.Auth.GitHub.TokenHeader)
	if tokenHeader == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub token required")
	}

	token := ExtractTokenFromHeader(tokenHeader)
	if token == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid GitHub token format")
	}

	// Create credentials for token-based authentication
	credentials := &services.Credentials{
		Type:  services.CredentialTypeToken,
		Token: token,
	}

	// Use internal auth service to authenticate
	user, err := authService.AuthenticateUser(c.Request().Context(), credentials)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed")
	}

	return user, nil
}

// GetConfigFromContext retrieves config from Echo context
func GetConfigFromContext(c echo.Context) *config.Config {
	if cfg := c.Get("config"); cfg != nil {
		if configObj, ok := cfg.(*config.Config); ok {
			return configObj
		}
	}
	return nil
}

// isOAuthEndpoint checks if the given path is an OAuth endpoint that should skip auth
func isOAuthEndpoint(path string) bool {
	if strings.HasPrefix(path, "/internal/sessions/") && strings.HasSuffix(path, "/github-credentials") {
		return true
	}
	oauthPaths := []string{
		"/oauth/authorize",
		"/oauth/callback",
		"/oauth/logout",
		"/oauth/refresh",
		"/auth/github-connections/callback",
		"/github-connections/login-options",
		"/github-connections/login",
		"/auth/google-connections/callback",
		"/google-connections/login-options",
		"/google-connections/login",
	}

	for _, oauthPath := range oauthPaths {
		if strings.HasPrefix(path, oauthPath) {
			return true
		}
	}
	return false
}

// isWebhookReceiverEndpoint checks if the given path is a webhook receiver endpoint
// These endpoints use HMAC signature verification instead of standard authentication
func isWebhookReceiverEndpoint(path string) bool {
	// Webhook receiver endpoints (not management endpoints)
	if strings.HasPrefix(path, "/hooks/") {
		return true
	}
	// Session manager forwarding endpoint — uses HMAC-SHA256 signature verification
	if strings.HasPrefix(path, "/api/v1/sessions") {
		return true
	}
	return false
}

// extractAPIKeyFromAuthHeader extracts API key from Authorization header
func extractAPIKeyFromAuthHeader(header string) string {
	if header == "" {
		return ""
	}

	// Handle "Bearer <token>" format
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}

	// Handle raw token
	return header
}

// skipAuthForHMACRequest checks if the request carries a valid HMAC-SHA256 signature.
// Used to allow trusted 親プロキシ requests to bypass standard authentication on External Session Manager.
// The signature must cover the canonical message: METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
func skipAuthForHMACRequest(c echo.Context, hmacSecret string) bool {
	sig := c.Request().Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return false
	}

	ts := c.Request().Header.Get(hmacutil.TimestampHeader)
	if ts == "" {
		return false
	}

	// Validate timestamp to prevent replay attacks
	if err := hmacutil.ValidateTimestamp(ts); err != nil {
		return false
	}

	// For non-GET/DELETE requests we read the body; for GET/DELETE use empty body
	var body []byte
	if c.Request().ContentLength > 0 || (c.Request().Method != http.MethodGet && c.Request().Method != http.MethodDelete) {
		var err error
		body, err = io.ReadAll(c.Request().Body)
		if err != nil {
			return false
		}
		c.Request().Body = io.NopCloser(bytes.NewReader(body))
	}

	// Build the canonical signing message: METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
	pathWithQuery := c.Request().URL.RequestURI()
	msg := hmacutil.BuildMessage(c.Request().Method, pathWithQuery, ts, body)

	return hmacutil.Verify([]byte(hmacSecret), msg, sig)
}
