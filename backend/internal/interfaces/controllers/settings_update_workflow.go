package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

type settingsUpdateEffects struct {
	removed []entities.ExternalSessionManagerEntry
	updated []externalManagerUpdate
}

func (c *SettingsController) loadSettingsUpdate(ctx echo.Context, name string) (*entities.Settings, bool, error) {
	// Get existing settings or create new one
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	isNewSettings := err != nil
	if isNewSettings {
		exists, checkErr := c.repo.Exists(ctx.Request().Context(), name)
		if checkErr != nil || exists {
			return nil, false, echo.NewHTTPError(http.StatusInternalServerError, "Failed to load settings")
		}
		// Create new settings if not exists
		settings = entities.NewSettings(name)
	}

	return settings, isNewSettings, nil
}

func (c *SettingsController) applySettingsUpdate(settings *entities.Settings, req *UpdateSettingsRequest, user *entities.User, isNewSettings bool) (settingsUpdateEffects, error) {
	if err := updateModelConnections(settings, req); err != nil {
		return settingsUpdateEffects{}, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	c.applyBedrockSettings(settings, req)

	c.applyMCPSettings(settings, req)

	c.applyMarketplaceSettings(settings, req)

	c.applyBasicSettings(settings, req)

	c.applyPreferredTeam(settings, req, user, isNewSettings)

	if err := c.applyGitHubInstallation(settings, req); err != nil {
		return settingsUpdateEffects{}, err
	}

	c.applySlackSettings(settings, req, user)

	c.applyNotificationChannels(settings, req, user)

	removedExternalManagers, updatedExternalManagers, err := c.applyExternalManagerSettings(settings, req)
	if err != nil {
		return settingsUpdateEffects{}, err
	}

	if err := c.applySessionDefaults(settings, req); err != nil {
		return settingsUpdateEffects{}, err
	}

	// Determine and set auth_mode
	authMode := c.determineAuthMode(settings, req.AuthMode)
	settings.SetAuthMode(authMode)

	// Validate
	if err := settings.Validate(); err != nil {
		return settingsUpdateEffects{}, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	return settingsUpdateEffects{removed: removedExternalManagers, updated: updatedExternalManagers}, nil
}

func (c *SettingsController) saveSettingsUpdate(ctx echo.Context, settings *entities.Settings, effects settingsUpdateEffects) error {
	removedExternalManagers, updatedExternalManagers := effects.removed, effects.updated
	// Save
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save settings")
	}
	for _, manager := range removedExternalManagers {
		if err := c.deleteExternalManagerRegistration(ctx.Request().Context(), manager); err != nil {
			return echo.NewHTTPError(http.StatusConflict, "settings saved but manager cleanup failed").SetInternal(err)
		}
	}
	for _, update := range updatedExternalManagers {
		if err := c.syncExternalManager(ctx.Request().Context(), update.current); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "settings saved but manager sync failed").SetInternal(err)
		}
	}

	return ctx.JSON(http.StatusOK, c.toResponse(settings))
}
