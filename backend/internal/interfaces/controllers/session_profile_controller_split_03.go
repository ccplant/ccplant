package controllers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func validateProfileSettingsTeam(user *entities.User, profile *entities.SessionProfile, cfg entities.SessionProfileConfig) error {
	team := cfg.SettingsTeamID()
	if team == "" {
		return nil
	}
	if profile.Scope() == entities.ScopeTeam && profile.TeamID() != team {
		return echo.NewHTTPError(http.StatusBadRequest, "team profiles can inherit settings only from their own team")
	}
	if !user.IsAdmin() && !user.IsMemberOfTeam(team) {
		return echo.NewHTTPError(http.StatusForbidden, "team membership is required to inherit settings")
	}
	return nil
}

func (c *SessionProfileController) validateProfileSecrets(ctx context.Context, profile *entities.SessionProfile, cfg entities.SessionProfileConfig) error {
	ids := cfg.SecretIDs()
	if len(ids) == 0 {
		return nil
	}
	settingsName := cfg.SettingsTeamID()
	if settingsName == "" {
		if profile.Scope() == entities.ScopeTeam {
			settingsName = profile.TeamID()
		} else {
			settingsName = profile.UserID()
		}
	}
	settings, err := c.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "referenced settings secrets were not found")
	}
	available := make(map[string]struct{})
	for _, secret := range settings.SecretSettings() {
		available[secret.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "secret_ids must not contain empty values")
		}
		if _, duplicate := seen[id]; duplicate {
			return echo.NewHTTPError(http.StatusBadRequest, "secret_ids must not contain duplicates")
		}
		seen[id] = struct{}{}
		if _, ok := available[id]; !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "referenced settings secret was not found")
		}
	}
	return nil
}

func (c *SessionProfileController) validateSourceSessionProfile(
	ctx context.Context,
	profile *entities.SessionProfile,
	cfg entities.SessionProfileConfig,
) error {
	sourceID := cfg.SourceSessionProfileID()
	if sourceID == "" {
		return nil
	}
	if sourceID == profile.ID() {
		return echo.NewHTTPError(http.StatusBadRequest, "a session profile cannot reference itself")
	}

	visited := map[string]struct{}{profile.ID(): {}}
	for sourceID != "" {
		if _, cyclic := visited[sourceID]; cyclic {
			return echo.NewHTTPError(http.StatusBadRequest, "session profile source contains a cyclic reference")
		}
		visited[sourceID] = struct{}{}

		source, err := c.repo.Get(ctx, sourceID)
		if err != nil {
			if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate session profile source")
		}
		if !sessionProfileSourceMatchesTenant(source, profile) {
			return echo.NewHTTPError(http.StatusForbidden, "session profile source belongs to another tenant")
		}
		sourceCfg := source.Config()
		sourceID = sourceCfg.SourceSessionProfileID()
	}
	return nil
}

func sessionProfileSourceMatchesTenant(source, profile *entities.SessionProfile) bool {
	switch profile.Scope() {
	case entities.ScopeUser:
		return source.Scope() == entities.ScopeUser && source.UserID() == profile.UserID()
	case entities.ScopeTeam:
		return source.Scope() == entities.ScopeTeam && source.TeamID() == profile.TeamID()
	default:
		return false
	}
}
