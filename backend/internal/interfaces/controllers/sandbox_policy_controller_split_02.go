package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type SandboxPolicyDomainsResponse struct {
	Allowed   []string `json:"allowed"`
	Denied    []string `json:"denied"`
	Ignored   []string `json:"ignored"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// UpdateIgnoredDomainsRequest is the JSON body for PUT /sandbox-policies/:id/domains/ignored.
type UpdateIgnoredDomainsRequest struct {
	Ignored []string `json:"ignored"`
}

// GetSandboxPolicyDomains handles GET /sandbox-policies/:id/domains.
// Returns the aggregated domain log collected by the background worker.
func (c *SandboxPolicyController) GetSandboxPolicyDomains(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	if c.domainRepo == nil {
		return echo.NewHTTPError(http.StatusNotImplemented, "Domain collection not available")
	}

	policy, err := c.repo.GetByID(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get sandbox policy")
	}
	if !c.canAccess(user, policy) {
		return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
	}

	data, err := c.domainRepo.Get(ctx.Request().Context(), policy.ID())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to read domain data")
	}

	if data == nil {
		return ctx.JSON(http.StatusOK, SandboxPolicyDomainsResponse{
			Allowed: []string{},
			Denied:  []string{},
			Ignored: []string{},
		})
	}

	resp := SandboxPolicyDomainsResponse{
		Allowed:   data.Allowed,
		Denied:    data.Denied,
		Ignored:   data.Ignored,
		UpdatedAt: data.UpdatedAt.Format(time.RFC3339),
	}
	if resp.Allowed == nil {
		resp.Allowed = []string{}
	}
	if resp.Denied == nil {
		resp.Denied = []string{}
	}
	if resp.Ignored == nil {
		resp.Ignored = []string{}
	}
	return ctx.JSON(http.StatusOK, resp)
}

// --- Access control ---

func (c *SandboxPolicyController) canCreate(user *entities.User, scope, teamID string) bool {
	if scope == string(entities.ScopeUser) {
		return true
	}
	return c.isMemberOfTeam(user, teamID)
}

func (c *SandboxPolicyController) canAccess(user *entities.User, policy *entities.SandboxPolicy) bool {
	switch policy.Scope() {
	case entities.ScopeUser:
		return string(user.ID()) == policy.OwnerID()
	case entities.ScopeTeam:
		return c.isMemberOfTeam(user, policy.TeamID())
	default:
		return false
	}
}

func (c *SandboxPolicyController) canModify(user *entities.User, policy *entities.SandboxPolicy) bool {
	return c.canAccess(user, policy)
}

func (c *SandboxPolicyController) isMemberOfTeam(user *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

func (c *SandboxPolicyController) userTeamIDs(user *entities.User) []string {
	if user.UserType() == entities.UserTypeServiceAccount {
		if user.TeamID() != "" {
			return []string{user.TeamID()}
		}
		return nil
	}
	githubInfo := user.GitHubInfo()
	if githubInfo == nil {
		return nil
	}
	teams := githubInfo.Teams()
	teamIDs := make([]string, 0, len(teams))
	for _, team := range teams {
		teamIDs = append(teamIDs, team.Organization+"/"+team.TeamSlug)
	}
	return teamIDs
}

func (c *SandboxPolicyController) toResponse(p *entities.SandboxPolicy) *SandboxPolicyResponse {
	return &SandboxPolicyResponse{
		ID:             p.ID(),
		Name:           p.Name(),
		Description:    p.Description(),
		AllowedDomains: p.AllowedDomains(),
		DeniedDomains:  p.DeniedDomains(),
		CountMode:      p.CountMode(),
		Scope:          string(p.Scope()),
		OwnerID:        p.OwnerID(),
		TeamID:         p.TeamID(),
		CreatedAt:      p.CreatedAt().Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt().Format(time.RFC3339),
	}
}
