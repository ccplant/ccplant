package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (c *SessionController) DeleteSession(ctx echo.Context) error {
	if err := c.checkRestartHold(ctx); err != nil {
		return err
	}
	c.setCORSHeaders(ctx)

	sessionID := ctx.Param("sessionId")
	clientIP := ctx.RealIP()

	log.Printf("Request: DELETE /sessions/%s from %s", sessionID, clientIP)

	if sessionID == "" {
		log.Printf("Delete session failed: missing session ID from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	session := c.getSessionManager().GetSession(sessionID)
	uncreatedAllocation := false
	if session == nil {
		// Check if it's a remote session
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
			if err != nil {
				log.Printf("Delete session: failed to look up route for %s: %v", sessionID, err)
			} else if route != nil {
				if route.Status == "templated" {
					authzCtx := auth.GetAuthorizationContext(ctx)
					if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
						return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
					}
					return ctx.JSON(http.StatusGone, map[string]string{"error": "session_templated", "template_id": route.ContextTemplateID})
				}
				if route.ManagerID == "" && route.RemoteSessionID != "" {
					return c.deleteLocalSessionAlias(ctx, route)
				}
				return c.deleteRemoteSession(ctx, route)
			}
		}
		session = findUncreatedSessionAllocation(c.getSessionManager().ListSessions(entities.SessionFilter{}), sessionID)
		if session == nil {
			// DELETE is idempotent. The UI may still hold a stale search result after
			// an orphaned route or allocation has already been cleaned up. Treating
			// that state as success lets the client remove the stale entry without
			// exposing whether an unknown session ID ever existed.
			log.Printf("Delete session: session %s is already absent (requested by %s)", sessionID, clientIP)
			c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)
			return ctx.JSON(http.StatusOK, map[string]interface{}{
				"message":    "Session already absent",
				"session_id": sessionID,
				"status":     "terminated",
			})
		}
		uncreatedAllocation = true
	}

	// Check authorization using pre-resolved authorization context (guaranteed to be non-nil by AuthMiddleware)
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		log.Printf("Delete session failed: user does not have access to session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
	}

	log.Printf("Deleting session %s (status: %s, user: %s) requested by %s",
		sessionID, session.Status(), session.UserID(), clientIP)
	deletePath := "managed_session"
	if uncreatedAllocation {
		deletePath = "pending_allocation"
	}
	usageDimensions := c.resolveSessionUsageDimensions(ctx.Request().Context(), sessionID, deletePath)

	if uncreatedAllocation {
		deleter, ok := c.sessionCreator.(pendingSessionAllocationDeleter)
		if !ok {
			log.Printf("Failed to delete pending session allocation %s: deletion is unsupported", sessionID)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session allocation")
		}
		deleted, err := deleter.DeletePendingSessionAllocation(ctx.Request().Context(), sessionID)
		if err != nil {
			log.Printf("Failed to delete pending session allocation %s: %v", sessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session allocation")
		}
		if !deleted {
			log.Printf("Pending session allocation %s was claimed before deletion", sessionID)
			return echo.NewHTTPError(http.StatusConflict, "Session allocation is no longer pending")
		}
		c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "pending_allocation", time.Now())
		log.Printf("Pending session allocation %s deletion completed successfully", sessionID)
		c.revokeGitHubBrokerLeases(ctx.Request().Context(), sessionID)
		c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Session allocation deleted successfully",
			"session_id": sessionID,
			"status":     "terminated",
		})
	}

	if err := c.sessionCreator.DeleteSessionByID(sessionID); err != nil {
		log.Printf("Failed to delete session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
	}
	c.recordSessionTerminated(ctx.Request().Context(), usageDimensions, "managed_session", time.Now())

	log.Printf("Session %s deletion completed successfully", sessionID)
	c.revokeGitHubBrokerLeases(ctx.Request().Context(), sessionID)
	c.cleanupSessionConfiguration(ctx.Request().Context(), sessionID)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
	})
}

// DeleteSessionFromWorker runs the durable deletion path for a request that was
// already authenticated by WorkerControlController. Worker cleanup is allowed
// to delete expired sessions regardless of their user or team owner.
func (c *SessionController) DeleteSessionFromWorker(ctx echo.Context) error {
	ctx.Set(workerAuthorizedDeleteContextKey, true)
	return c.DeleteSession(ctx)
}

func (c *SessionController) revokeGitHubBrokerLeases(ctx context.Context, sessionID string) {
	if c.githubTokenResolver == nil {
		return
	}
	if err := c.githubTokenResolver.RevokeBrokerLeases(ctx, sessionID); err != nil {
		log.Printf("Failed to revoke GitHub broker lease for session %s: %v", sessionID, err)
	}
}

func findUncreatedSessionAllocation(sessions []entities.Session, sessionID string) entities.Session {
	for _, session := range sessions {
		if session.ID() == sessionID && (session.Status() == "pending" || session.Status() == "allocating" || session.Status() == "error") {
			return session
		}
	}
	return nil
}

// ResumeSession explicitly recreates a suspended session workload. Read-only
// status, message, and SSE endpoints deliberately do not wake a session.
func (c *SessionController) ResumeSession(ctx echo.Context) error {
	if err := c.checkRestartHold(ctx); err != nil {
		return err
	}
	sessionID := ctx.Param("sessionId")
	workloadSessionID := sessionID
	session := c.getSessionManager().GetSession(sessionID)
	if session == nil && c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to look up session route")
		}
		if route != nil {
			if route.ManagerID != "" {
				return c.resumeRemoteSession(ctx, route)
			}
			if route.RemoteSessionID != "" {
				workloadSessionID = route.RemoteSessionID
				session = c.getSessionManager().GetSession(workloadSessionID)
			}
		}
	}
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}
	ensurer, ok := c.getSessionManager().(repositories.SessionWorkloadEnsurer)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "Session resume is not supported by this session manager")
	}
	ensured, restoring, err := ensurer.EnsureSessionWorkload(ctx.Request().Context(), workloadSessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("failed to restore session workload: %v", err))
	}
	status := "active"
	code := http.StatusOK
	if restoring {
		status = "resuming"
		code = http.StatusAccepted
		ctx.Response().Header().Set("Retry-After", "2")
	} else if ensured != nil {
		status = ensured.Status()
	}
	return ctx.JSON(code, map[string]interface{}{"session_id": sessionID, "status": status})
}

// SuspendSession asks the owning session manager to suspend the workload. The
// manager owns any checkpoint policy and does not expose it through this API.
func (c *SessionController) SuspendSession(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	session := c.getSessionManager().GetSession(sessionID)
	if session == nil && c.sessionRouteRepo != nil {
		route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to look up session route")
		}
		if route != nil && route.ManagerID != "" {
			return c.suspendRemoteSession(ctx, route)
		}
	}
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}
	suspender, ok := c.getSessionManager().(repositories.SessionSuspender)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "Session suspend is not supported by this session manager")
	}
	if err := suspender.SuspendSession(ctx.Request().Context(), sessionID); err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, fmt.Sprintf("failed to suspend session: %v", err))
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"session_id": sessionID, "status": "suspended"})
}

func (c *SessionController) suspendRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx == nil || !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}
	if route.RemoteSessionID == "" || c.esmControlTunnel == nil || !c.esmControlTunnel.IsConnected(ctx.Request().Context(), route.ManagerID) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager outbound control connection is unavailable")
	}
	targetURL := "http://esm.local/api/v1/sessions/" + url.PathEscape(route.RemoteSessionID) + "/suspend"
	if c.sessionRunnerStore == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Session resume data store is unavailable")
	}
	allocation, err := c.sessionRunnerStore.GetAllocation(ctx.Request().Context(), route.SessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "Session resume data is unavailable; the session was not suspended")
	}
	if len(allocation.ProvisionSettings) == 0 || strings.TrimSpace(allocation.RuntimeToken) == "" || allocation.Generation <= 0 {
		return echo.NewHTTPError(http.StatusConflict, "Session resume data is incomplete; the session was not suspended")
	}
	var settings sessionsettings.SessionSettings
	if err := json.Unmarshal(allocation.ProvisionSettings, &settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to decode session resume data; the session was not suspended")
	}
	settings.ParentRuntime = refreshedParentRuntime(ctx, settings.ParentRuntime, route, allocation)
	settingsBody, err := json.Marshal(&settings)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session resume data; the session was not suspended")
	}
	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, targetURL, bytes.NewReader(settingsBody))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to build suspend request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.esmControlTunnel.Do(ctx.Request().Context(), route.ManagerID, route.SessionID, route.RemoteSessionID, req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to reach external session manager")
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return echo.NewHTTPError(resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := c.recordRemoteLifecycleStatus(ctx.Request().Context(), route, "suspended"); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to persist suspended session status")
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"session_id": route.SessionID, "status": "suspended"})
}

// recordRemoteLifecycleStatus keeps the public route and only the allocated
// session's local cache entry in sync after a remote lifecycle command.
func (c *SessionController) recordRemoteLifecycleStatus(ctx context.Context, route *repositories.SessionRoute, status string) error {
	if err := c.RecordRemoteSessionStatus(ctx, route, status); err != nil {
		return err
	}
	if session := c.getSessionManager().GetSession(route.RemoteSessionID); session != nil {
		if updater, ok := session.(sessionStatusCacheUpdater); ok {
			updater.SetStatusSilent(status)
		}
	}
	return nil
}

// RouteToSession routes requests to the appropriate agentapi server instance
