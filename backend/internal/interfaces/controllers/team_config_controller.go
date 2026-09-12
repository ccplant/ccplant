package controllers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type TeamConfigController struct {
	repo repositories.TeamConfigRepository
}

type TeamConfigResponse struct {
	TeamID        string                         `json:"team_id"`
	PrincipalID   string                         `json:"principal_id"`
	ExternalTeams []entities.ExternalTeamBinding `json:"external_teams"`
}

type updateTeamConfigRequest struct {
	ExternalTeams []entities.ExternalTeamBinding `json:"external_teams"`
}

func NewTeamConfigController(repo repositories.TeamConfigRepository) *TeamConfigController {
	return &TeamConfigController{repo: repo}
}

func (c *TeamConfigController) Get(ctx echo.Context) error {
	teamID := ctx.Param("team")
	if !canManageTeam(ctx, teamID) {
		return echo.NewHTTPError(http.StatusForbidden, "team access denied")
	}
	team, err := c.repo.FindByTeamID(ctx.Request().Context(), teamID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "team config not found")
	}
	return ctx.JSON(http.StatusOK, teamConfigResponse(team))
}

func (c *TeamConfigController) Update(ctx echo.Context) error {
	teamID := ctx.Param("team")
	if !canManageTeam(ctx, teamID) {
		return echo.NewHTTPError(http.StatusForbidden, "team access denied")
	}
	var request updateTeamConfigRequest
	if err := ctx.Bind(&request); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	team, err := c.repo.FindByTeamID(ctx.Request().Context(), teamID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "team config not found")
	}

	bindings := make([]entities.ExternalTeamBinding, 0, len(team.ExternalTeams())+len(request.ExternalTeams))
	for _, binding := range team.ExternalTeams() {
		if binding.ManagedBy == "discovery" {
			bindings = append(bindings, binding)
		}
	}
	seen := make(map[string]struct{}, len(bindings)+len(request.ExternalTeams))
	for _, binding := range bindings {
		seen[bindingKey(binding)] = struct{}{}
	}
	for _, binding := range request.ExternalTeams {
		binding.ConnectionID = strings.TrimSpace(binding.ConnectionID)
		binding.Organization = strings.ToLower(strings.TrimSpace(binding.Organization))
		binding.TeamSlug = strings.ToLower(strings.TrimSpace(binding.TeamSlug))
		binding.ManagedBy = "api"
		if binding.ConnectionID == "" || binding.Organization == "" || binding.TeamSlug == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "connection_id, organization, and team_slug are required")
		}
		key := bindingKey(binding)
		if _, exists := seen[key]; exists {
			return echo.NewHTTPError(http.StatusConflict, "external GitHub team is already mapped")
		}
		seen[key] = struct{}{}
		bindings = append(bindings, binding)
	}
	allTeams, err := c.repo.List(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate team mappings").SetInternal(err)
	}
	for _, other := range allTeams {
		if other.TeamID() == teamID {
			continue
		}
		for _, otherBinding := range other.ExternalTeams() {
			if _, exists := seen[bindingKey(otherBinding)]; exists {
				return echo.NewHTTPError(http.StatusConflict, "external GitHub team is already mapped to another ccplant team")
			}
		}
	}
	sort.Slice(bindings, func(i, j int) bool { return bindingKey(bindings[i]) < bindingKey(bindings[j]) })
	team.SetExternalTeams(bindings)
	if err := c.repo.Save(ctx.Request().Context(), team); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save team config").SetInternal(err)
	}
	return ctx.JSON(http.StatusOK, teamConfigResponse(team))
}

func canManageTeam(ctx echo.Context, teamID string) bool {
	authz := auth.GetAuthorizationContext(ctx)
	return authz != nil && (authz.TeamScope.IsAdmin || authz.CanAccessTeam(teamID))
}

func bindingKey(binding entities.ExternalTeamBinding) string {
	return binding.ConnectionID + "\x00" + strings.ToLower(binding.Organization) + "\x00" + strings.ToLower(binding.TeamSlug)
}

func teamConfigResponse(team *entities.TeamConfig) TeamConfigResponse {
	bindings := team.ExternalTeams()
	if bindings == nil {
		bindings = []entities.ExternalTeamBinding{}
	}
	return TeamConfigResponse{TeamID: team.TeamID(), PrincipalID: team.PrincipalID(), ExternalTeams: bindings}
}
