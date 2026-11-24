package controllers

import (
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
	"github.com/takutakahashi/agentapi-proxy/pkg/urlutil"
)

// BaseSettingsName is the reserved name for global base settings (admin-only)
const BaseSettingsName = "base"

// SettingsController handles settings-related HTTP requests
type SettingsController struct {
	repo               repositories.SettingsRepository
	notificationSvc    *notification.Service // Optional
	esmMu              sync.Mutex
	esmControlTunnel   ESMControlTunnel
	sessionRunnerStore sessionrunnercore.Store
}

func (c *SettingsController) SetSessionRunnerStore(store sessionrunnercore.Store) {
	c.sessionRunnerStore = store
}

func (c *SettingsController) SetESMControlTunnel(tunnel ESMControlTunnel) {
	c.esmControlTunnel = tunnel
}

// NewSettingsController creates new settings controller
func NewSettingsController(repo repositories.SettingsRepository, notificationSvc *notification.Service) *SettingsController {
	return &SettingsController{
		repo:            repo,
		notificationSvc: notificationSvc,
	}
}

// GetName returns the name of this controller for logging
func (c *SettingsController) GetName() string {
	return "SettingsController"
}

// GetAvailableManagers handles GET /settings/managers
// Returns all external session managers available to the authenticated user:
// managers from their own settings plus managers from every team they belong to.
// HMAC secrets are NOT included in this response.
func (c *SettingsController) GetAvailableManagers(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	managers := make([]AvailableManagerEntry, 0)
	managers = append(managers, c.settingsManagerEntries(ctx, string(user.ID()), "user")...)
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			id := team.Organization + "/" + team.TeamSlug
			managers = append(managers, c.settingsManagerEntries(ctx, id, "team")...)
		}
	}
	return ctx.JSON(http.StatusOK, &AvailableManagersResponse{Managers: managers})
}

func (c *SettingsController) settingsManagerEntries(ctx echo.Context, name, source string) []AvailableManagerEntry {
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		return nil
	}
	entries := make([]AvailableManagerEntry, 0)
	for _, manager := range settings.ExternalSessionManagers() {
		if manager.HMACSecret == "" || !manager.IsPoolEnabled() || manager.Pool == "" {
			continue
		}
		entries = append(entries, AvailableManagerEntry{ID: manager.ID, Name: manager.Name, Pool: manager.Pool, PoolEnabled: manager.IsPoolEnabled(), AutomaticAssignmentEnabled: manager.IsAutomaticAssignmentEnabled(), Source: source, SourceName: name})
	}
	return entries
}

// GetSettings handles GET /settings/:name
func (c *SettingsController) GetSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := urlutil.DecodeSlashParam(ctx.Param("name"))
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check access permission
	if !c.canAccess(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get settings")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(settings))
}

// UpdateSettings handles PUT /settings/:name
func (c *SettingsController) UpdateSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := urlutil.DecodeSlashParam(ctx.Param("name"))
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateSettingsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if err := c.validateEnabledTeamProfiles(name, user, &req); err != nil {
		return err
	}

	settings, isNewSettings, err := c.loadSettingsUpdate(ctx, name)
	if err != nil {
		return err
	}
	effects, err := c.applySettingsUpdate(settings, &req, user, isNewSettings)
	if err != nil {
		return err
	}
	return c.saveSettingsUpdate(ctx, settings, effects)
}

// DeleteSettings handles DELETE /settings/:name
