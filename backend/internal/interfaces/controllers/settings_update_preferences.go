package controllers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (c *SettingsController) validateEnabledTeamProfiles(name string, user *entities.User, req *UpdateSettingsRequest) error {
	if req.EnabledTeamProfileIDs != nil {
		if name != string(user.ID()) {
			return echo.NewHTTPError(http.StatusBadRequest, "enabled_team_session_profile_ids is available only in personal settings")
		}
		if len(*req.EnabledTeamProfileIDs) > 100 {
			return echo.NewHTTPError(http.StatusBadRequest, "enabled_team_session_profile_ids must contain at most 100 items")
		}
		seen := make(map[string]struct{}, len(*req.EnabledTeamProfileIDs))
		for _, id := range *req.EnabledTeamProfileIDs {
			if strings.TrimSpace(id) == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "enabled_team_session_profile_ids must not contain empty values")
			}
			if _, duplicate := seen[id]; duplicate {
				return echo.NewHTTPError(http.StatusBadRequest, "enabled_team_session_profile_ids must not contain duplicates")
			}
			seen[id] = struct{}{}
		}
	}
	return nil
}

func (c *SettingsController) applyPreferredTeam(settings *entities.Settings, req *UpdateSettingsRequest, user *entities.User, isNewSettings bool) {
	// Update preferred team ID
	if req.PreferredTeamID != nil {
		// Explicitly specified (empty string "" clears the setting)
		settings.SetPreferredTeamID(*req.PreferredTeamID)
	} else if isNewSettings {
		// Auto-select on first creation: if user belongs to exactly one team, set it
		if user.GitHubInfo() != nil {
			teams := user.GitHubInfo().Teams()
			if len(teams) == 1 {
				teamID := teams[0].Organization + "/" + teams[0].TeamSlug
				settings.SetPreferredTeamID(teamID)
			}
		}
	}
}

func (c *SettingsController) applyGitHubInstallation(settings *entities.Settings, req *UpdateSettingsRequest) error {
	if req.GitHubAppInstallationID != nil {
		installationID := strings.TrimSpace(*req.GitHubAppInstallationID)
		if installationID != "" {
			parsedID, err := strconv.ParseInt(installationID, 10, 64)
			if err != nil || parsedID <= 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "github_app_installation_id must be a positive integer")
			}
		}
		settings.SetGitHubAppInstallationID(installationID)
	}
	return nil
}

func (c *SettingsController) applySessionDefaults(settings *entities.Settings, req *UpdateSettingsRequest) error {
	if req.DefaultSessionProfileID != nil {
		settings.SetDefaultSessionProfileID(*req.DefaultSessionProfileID)
	}
	if req.EnabledTeamProfileIDs != nil {
		settings.SetEnabledTeamSessionProfileIDs(*req.EnabledTeamProfileIDs)
	}
	if req.GitHubRepositories != nil {
		repositories, err := normalizeRepositoryPatterns(*req.GitHubRepositories)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		settings.SetGitHubRepositories(repositories)
	}
	if req.DefaultAgentType != nil {
		if !isValidDefaultAgentType(*req.DefaultAgentType) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid default_agent_type")
		}
		settings.SetDefaultAgentType(*req.DefaultAgentType)
	}
	if req.AutoSuspend != nil {
		if err := req.AutoSuspend.Validate(); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid_auto_suspend_settings: "+err.Error())
		}
		settings.SetAutoSuspend(req.AutoSuspend)
	}
	return nil
}
