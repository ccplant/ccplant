package controllers

import (
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Apply before routing (including direct-runtime/external-manager routes).
// Shared-session links intentionally do not expose the Herdr control surface.
func (c *SessionController) authorizeHerdrRequest(ctx echo.Context) error {
	parts := strings.SplitN(path.Clean(ctx.Request().URL.Path), "/", 3)
	if len(parts) < 3 || (parts[2] != "herdr" && !strings.HasPrefix(parts[2], "herdr/")) || ctx.Request().Method == http.MethodOptions {
		return nil
	}
	a := auth.GetAuthorizationContext(ctx)
	if a == nil || a.User == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	permission := entities.PermissionSessionRead
	if ctx.Request().Method != http.MethodGet {
		permission = entities.PermissionSessionCreate
	}
	if !a.User.HasPermission(permission) {
		return echo.NewHTTPError(http.StatusForbidden, "Insufficient permissions")
	}
	id := ctx.Param("sessionId")
	if session := c.getSessionManager().GetSession(id); session != nil {
		if !a.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
			return echo.NewHTTPError(http.StatusForbidden, "Session access denied")
		}
		return nil
	}
	if c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), id)
		if err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Session route unavailable")
		}
		if route != nil && a.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
			return nil
		}
	}
	return echo.NewHTTPError(http.StatusForbidden, "Session access denied")
}
