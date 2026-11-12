package controllers

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type sessionOperationalLogProvider interface {
	OperationalLogs(context.Context, string, string, int) ([]string, string, error)
}

// GetSessionLogs returns the tail of the workload Pod logs for a session the
// authenticated user can access. Remote session logs are read through the
// owning session manager's control tunnel.
func (c *SessionController) GetSessionLogs(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	authzCtx := auth.GetAuthorizationContext(ctx)
	tail, _ := strconv.Atoi(ctx.QueryParam("tail"))
	if tail < 1 || tail > 5000 {
		tail = 200
	}

	if session := c.getSessionManager().GetSession(sessionID); session != nil {
		if authzCtx == nil || !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
			return echo.NewHTTPError(http.StatusForbidden, "access denied")
		}
		provider, ok := c.getSessionManager().(sessionOperationalLogProvider)
		if !ok {
			return echo.NewHTTPError(http.StatusNotImplemented, "session logs are unavailable")
		}
		lines, source, err := provider.OperationalLogs(ctx.Request().Context(), "", sessionID, tail)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to read session logs").SetInternal(err)
		}
		return ctx.JSON(http.StatusOK, map[string]interface{}{"lines": lines, "source": source})
	}

	if c.sessionRouteRepo == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to look up session route").SetInternal(err)
	}
	if route == nil || route.RemoteSessionID == "" {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	if c.esmControlTunnel == nil || !c.esmControlTunnel.IsConnected(ctx.Request().Context(), route.ManagerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "session manager control channel is offline")
	}

	query := url.Values{"session_id": {route.RemoteSessionID}, "tail": {strconv.Itoa(tail)}}
	req, _ := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet, "http://manager/internal/esm-management/logs?"+query.Encode(), nil)
	resp, err := c.esmControlTunnel.Do(ctx.Request().Context(), route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "session manager operation failed").SetInternal(err)
	}
	defer resp.Body.Close()
	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx.Response().WriteHeader(resp.StatusCode)
	_, err = io.Copy(ctx.Response(), resp.Body)
	return err
}
