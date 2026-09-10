package controllers

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// UserController handles user-related endpoints
type UserController struct {
	teamConfigRepo repositories.TeamConfigRepository
}

type UserTeamResponse struct {
	TeamID      string `json:"team_id"`
	PrincipalID string `json:"principal_id"`
}

// UserInfoResponse represents the response for /user/info endpoint
type UserInfoResponse struct {
	PrincipalID    string             `json:"principal_id"`
	Username       string             `json:"username"`
	Teams          []string           `json:"teams"`
	TeamPrincipals []UserTeamResponse `json:"team_principals,omitempty"`
	IsAdmin        bool               `json:"is_admin"`
}

// NewUserController creates a new UserController instance
func NewUserController(teamConfigRepo ...repositories.TeamConfigRepository) *UserController {
	controller := &UserController{}
	if len(teamConfigRepo) > 0 {
		controller.teamConfigRepo = teamConfigRepo[0]
	}
	return controller
}

// GetName returns the name of this controller for logging
func (c *UserController) GetName() string {
	return "UserController"
}

// GetUserInfo handles GET /user/info requests
func (c *UserController) GetUserInfo(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	response := UserInfoResponse{
		PrincipalID: string(user.ID()),
		Teams:       []string{},
		IsAdmin:     user.IsAdmin(),
	}
	if teamIDs, resolved := user.ResolvedTeamIDs(); resolved {
		response.Teams = teamIDs
	}

	if githubInfo := user.GitHubInfo(); githubInfo != nil {
		response.Username = githubInfo.Login()
		if _, resolved := user.ResolvedTeamIDs(); !resolved {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				response.Teams = append(response.Teams, teamSlug)
			}
		}
	} else {
		// Fallback for non-GitHub users (e.g., personal API key users)
		response.Username = user.Username()
	}
	if c.teamConfigRepo != nil {
		for _, teamID := range response.Teams {
			team, err := c.teamConfigRepo.FindByTeamID(ctx.Request().Context(), teamID)
			if err == nil {
				response.TeamPrincipals = append(response.TeamPrincipals, UserTeamResponse{TeamID: teamID, PrincipalID: team.PrincipalID()})
			}
		}
	}

	return ctx.JSON(http.StatusOK, response)
}
