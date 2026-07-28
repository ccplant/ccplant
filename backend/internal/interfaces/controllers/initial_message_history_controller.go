package controllers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type InitialMessageHistoryController struct {
	service *sessionuc.InitialMessageHistoryService
}

func NewInitialMessageHistoryController(service *sessionuc.InitialMessageHistoryService) *InitialMessageHistoryController {
	return &InitialMessageHistoryController{service: service}
}

func (c *InitialMessageHistoryController) List(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	limit := sessionuc.InitialMessageHistoryLimit
	if raw := ctx.QueryParam("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "limit must be a positive integer")
		}
		limit = parsed
	}
	items, err := c.service.List(ctx.Request().Context(), string(user.ID()), limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get initial message history")
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"items": items})
}

func (c *InitialMessageHistoryController) DeleteAll(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	if err := c.service.DeleteAll(ctx.Request().Context(), string(user.ID())); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete initial message history")
	}
	return ctx.NoContent(http.StatusNoContent)
}
