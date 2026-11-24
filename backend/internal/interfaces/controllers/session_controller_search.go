package controllers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

func (c *SessionController) SearchSessions(ctx echo.Context) error {
	request := ctx.Request()
	return telemetry.OperationErr(request.Context(), "controllers.SessionController.SearchSessions", func(requestCtx context.Context) error {
		ctx.SetRequest(request.WithContext(requestCtx))
		return c.searchSessions(ctx)
	},
		telemetry.String("search.scope", ctx.QueryParam("scope")),
		telemetry.String("search.status", ctx.QueryParam("status")),
		telemetry.Bool("search.team_filter", ctx.QueryParam("team_id") != ""),
	)
}

func (c *SessionController) searchSessions(ctx echo.Context) error {
	c.setCORSHeaders(ctx)
	query := newSessionSearchQuery(ctx)
	matchingSessions := c.listMatchingSessions(query)
	routes, matchingSessions, allocatedSessions := c.listSearchRoutes(query, matchingSessions)
	filteredSessions := c.buildSearchResponse(query, matchingSessions, routes, allocatedSessions)
	telemetry.SetAttributes(ctx.Request().Context(), telemetry.Int64("search.result_count", int64(len(filteredSessions))))

	return telemetry.OperationErr(ctx.Request().Context(), "controllers.SessionController.SearchSessions.WriteResponse", func(context.Context) error {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"sessions": filteredSessions,
		})
	})
}
