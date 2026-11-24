package controllers

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *SessionController) UpdateSessionAnnotations(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	sessionID := ctx.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return c.updateRoutedSessionAnnotations(ctx, sessionID)
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this session")
	}

	var req entities.UpdateSessionAnnotationsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	updater, ok := c.getSessionManager().(sessionAnnotationUpdater)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "Session annotations are not supported")
	}

	annotations, err := updater.UpdateSessionAnnotations(ctx.Request().Context(), sessionID, req)
	if err != nil {
		log.Printf("Failed to update session annotations for %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update session annotations")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_id":  sessionID,
		"annotations": annotations,
		"metadata": map[string]interface{}{
			"description": annotations.Description,
		},
	})
}

func (c *SessionController) updateRoutedSessionAnnotations(ctx echo.Context, sessionID string) error {
	if c.sessionRouteRepo == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to resolve session route").SetInternal(err)
	}
	if route == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx == nil || !authzCtx.CanModifyResource(route.UserID, route.Scope, route.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this session")
	}

	var req entities.UpdateSessionAnnotationsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if req.PRURL != nil {
		route.Annotations.PRURL = *req.PRURL
	}
	if req.IssueURL != nil {
		route.Annotations.IssueURL = *req.IssueURL
	}
	if req.Description != nil {
		route.Annotations.Description = *req.Description
	}
	if req.RunningTask != nil {
		route.Annotations.RunningTask = *req.RunningTask
	}
	if err := c.sessionRouteRepo.Save(ctx.Request().Context(), route); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update session annotations").SetInternal(err)
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_id":  sessionID,
		"annotations": route.Annotations,
		"metadata": map[string]interface{}{
			"description": route.Annotations.Description,
		},
	})
}

// DeleteSession handles DELETE /sessions/:sessionId requests to terminate a session
