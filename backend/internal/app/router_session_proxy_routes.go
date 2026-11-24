package app

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (r *Router) registerSessionProxyRoutes() {
	// Session proxy route must be registered after all proxy-level routes so
	// paths such as /resources/transfer are not treated as session IDs.
	r.echo.Any("/:sessionId/*", r.handlers.sessionController.RouteToSession)

	// Add explicit OPTIONS handler for session proxy routes to ensure CORS preflight works
	r.echo.OPTIONS("/:sessionId/*", func(c echo.Context) error {
		// Set CORS headers for preflight
		c.Response().Header().Set("Access-Control-Allow-Origin", "*")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
		c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
		c.Response().Header().Set("Access-Control-Max-Age", "86400")
		return c.NoContent(http.StatusNoContent)
	})
}

// registerConditionalRoutes registers routes based on server configuration
