package api_token

import (
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func validatePermissionsSubset(caller *entities.User, requested []entities.Permission) error {
	for _, p := range requested {
		if !caller.HasPermission(p) {
			return fmt.Errorf("requested permission %q is not granted to the caller", p)
		}
	}
	return nil
}

// canAccessTeam reports whether the caller may access a team's resources:
// admins qualify, as do team members (GitHub teams) and team service accounts.
func canAccessTeam(caller *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if caller.IsAdmin() {
		return true
	}
	return caller.IsMemberOfTeam(teamID)
}

// canAccessToken reports whether the caller may read a token's metadata.
func canAccessToken(caller *entities.User, token *entities.APIToken) bool {
	switch token.Scope() {
	case entities.APITokenScopeUser:
		// Strictly owner-only; admin has no special bypass for personal tokens.
		return caller.ID() == token.UserID()
	case entities.APITokenScopeTeam:
		return canAccessTeam(caller, token.TeamID())
	default:
		return false
	}
}

// canDeleteToken reports whether the caller may delete a token.
//   - Personal tokens: only the owner.
//   - Team tokens: the creator or an admin.
func canDeleteToken(caller *entities.User, token *entities.APIToken) bool {
	switch token.Scope() {
	case entities.APITokenScopeUser:
		return caller.ID() == token.UserID()
	case entities.APITokenScopeTeam:
		if caller.IsAdmin() {
			return true
		}
		return caller.ID() == token.CreatedBy()
	default:
		return false
	}
}

// serviceAccountUserID returns the canonical service-account user id for a
// team, matching the format used by SimpleAuthService.CreateServiceAccountForTeam.
func serviceAccountUserID(teamID string) entities.UserID {
	return entities.UserID(fmt.Sprintf("sa-%s", strings.ReplaceAll(teamID, "/", "-")))
}

// callerTeamIDs returns the team ids the caller belongs to. For service
// accounts it is the single team; for GitHub users it is derived from their
// team memberships.
func callerTeamIDs(caller *entities.User) []string {
	if caller.UserType() == entities.UserTypeServiceAccount {
		if caller.TeamID() != "" {
			return []string{caller.TeamID()}
		}
		return nil
	}
	if gh := caller.GitHubInfo(); gh != nil {
		teams := gh.Teams()
		out := make([]string, 0, len(teams))
		for _, t := range teams {
			out = append(out, t.Organization+"/"+t.TeamSlug)
		}
		return out
	}
	return nil
}
