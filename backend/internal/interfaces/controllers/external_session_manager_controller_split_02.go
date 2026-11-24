package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *SettingsController) esmSettingsName(ctx echo.Context, scope, teamID string, modify bool) (string, error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "team" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "scope must be user or team")
	}
	name := string(user.ID())
	if scope == "team" {
		if teamID == "" {
			return "", echo.NewHTTPError(http.StatusBadRequest, "team_id is required")
		}
		name = teamID
		if !user.IsAdmin() && !user.IsMemberOfTeam(teamID) {
			return "", echo.NewHTTPError(http.StatusForbidden, "access denied")
		}
		return name, nil
	}
	allowed := c.canAccess(user, name)
	if modify {
		allowed = c.canModify(user, name)
	}
	if !allowed {
		return "", echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	return name, nil
}

func (c *SettingsController) findOrCreateSettings(ctx echo.Context, name string) (*entities.Settings, error) {
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err == nil {
		return settings, nil
	}
	if strings.Contains(err.Error(), "not found") {
		return entities.NewSettings(name), nil
	}
	return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to load settings")
}

func (c *SettingsController) findAuthorizedESM(ctx echo.Context, modify bool) (*entities.ExternalSessionManagerEntry, *entities.Settings, error) {
	name, err := c.esmSettingsName(ctx, ctx.QueryParam("scope"), ctx.QueryParam("team_id"), modify)
	if err != nil {
		return nil, nil, err
	}
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		return nil, nil, echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}
	for _, manager := range settings.ExternalSessionManagers() {
		if manager.ID == ctx.Param("id") {
			copyManager := manager
			return &copyManager, settings, nil
		}
	}
	return nil, nil, echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
}

func esmResponse(manager entities.ExternalSessionManagerEntry, token string) ExternalSessionManagerResponse {
	return ExternalSessionManagerResponse{ID: manager.ID, InstanceID: manager.InstanceID, Name: manager.Name,
		HasConnectionToken: manager.HMACSecret != "", ConnectionToken: token,
		Labels: manager.Labels, Version: manager.Version,
		ActiveSessions:  manager.ActiveSessions,
		LastHeartbeatAt: timePtrUnlessZero(manager.LastHeartbeatAt)}
}

func timePtrUnlessZero(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
