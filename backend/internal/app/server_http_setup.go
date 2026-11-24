package app

import (
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/takutakahashi/agentapi-proxy/pkg/urlutil"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

func newServerHTTP() *echo.Echo {
	e := echo.New()

	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)

	// Create server spans and HTTP RED metrics. Health probes are excluded to
	// avoid high-volume, low-value telemetry.
	e.Use(otelecho.Middleware("agentapi-proxy", otelecho.WithSkipper(func(c echo.Context) bool {
		switch c.Path() {
		case "/health", "/healthz", "/ready", "/readyz":
			return true
		default:
			return false
		}
	})))

	// Rewrite %2F in URL paths before route matching so that settings names
	// containing slashes (e.g. "org/team-slug") are routed correctly.
	e.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rawPath := c.Request().URL.RawPath
			if rawPath != "" {
				if rewritten, ok := urlutil.RewriteEncodedSlashes(rawPath); ok {
					c.Request().URL.Path = rewritten
					c.Request().URL.RawPath = rewritten
				}
			}
			return next(c)
		}
	})

	// Add recovery middleware
	e.Use(middleware.Recover())

	// Add security headers middleware
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		HSTSMaxAge:            31536000, // 1 year
		HSTSExcludeSubdomains: false,
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	}))

	// Add CORS middleware with secure configuration (only for non-proxy routes)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper:          skipServerCORS,
		AllowOriginFunc:  allowServerCORSOrigin,
		AllowMethods:     []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Requested-With", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-API-Key", "Acp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	return e
}

func skipServerCORS(c echo.Context) bool {
	// Skip CORS middleware only for proxy routes (/:sessionId/* pattern)
	// These routes handle CORS manually in the proxy
	path := c.Request().URL.Path
	pathParts := strings.Split(path, "/")
	// Skip CORS for shared session routes (/s/:shareToken/*)
	if len(pathParts) >= 3 && pathParts[1] == "s" {
		return true
	}
	// Skip CORS only for proxy routes that match /:sessionId/* pattern
	// (at least 3 parts, not starting with "start", "search", "sessions", "oauth", "auth", "notification", or "notifications")
	if len(pathParts) >= 3 && pathParts[1] != "" {
		firstSegment := pathParts[1]
		return firstSegment != "start" && firstSegment != "search" && firstSegment != "sessions" && firstSegment != "oauth" && firstSegment != "auth" && firstSegment != "notification" && firstSegment != "notifications" && firstSegment != "credentials" && firstSegment != "files" && firstSegment != "storages" && firstSegment != "session-profiles" && firstSegment != "sandbox-policies" && firstSegment != "integrations"
	}
	return false
}

func allowServerCORSOrigin(origin string) (bool, error) {
	// Get allowed origins from environment variable
	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		// Fallback to localhost for development
		allowed := strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "https://localhost") ||
			strings.HasPrefix(origin, "http://127.0.0.1") ||
			strings.HasPrefix(origin, "https://127.0.0.1")
		return allowed, nil
	}
	// Parse comma-separated allowed origins
	origins := strings.Split(allowedOrigins, ",")
	for _, allowed := range origins {
		if strings.TrimSpace(allowed) == origin {
			return true, nil
		}
	}
	return false, nil
}
