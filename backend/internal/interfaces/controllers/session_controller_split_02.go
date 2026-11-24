package controllers

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/executiontoken"
)

func (c *SessionController) startSession(ctx echo.Context) error {
	c.setCORSHeaders(ctx)
	dryRun := false
	if raw := ctx.QueryParam("dry_run"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "dry_run must be a boolean")
		}
		dryRun = parsed
	}

	sessionID := uuid.New().String()
	if claims, ok := ctx.Get("trigger_execution_claims").(executiontoken.ExecutionClaims); ok {
		sessionID = claims.SessionID
		if existing := c.getSessionManager().GetSession(sessionID); existing != nil {
			return ctx.JSON(http.StatusOK, map[string]interface{}{"session_id": sessionID})
		}
	}

	operation := &sessionStartOperation{controller: c, context: ctx, dryRun: dryRun, sessionID: sessionID}
	for _, phase := range []func() error{operation.bindInput, operation.resolveIdentity, operation.configureGitHub, operation.authorizeScope, operation.resolveContext} {
		if err := phase(); err != nil {
			return err
		}
	}
	if done, err := operation.resolveReuseAndInput(); done || err != nil {
		return err
	}
	for _, phase := range []func() error{operation.applyProfile, operation.persistInput} {
		if err := phase(); err != nil {
			return err
		}
	}
	if dryRun {
		return operation.preview()
	}
	return operation.create()
}
