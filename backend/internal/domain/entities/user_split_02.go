package entities

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func (u *User) RemovePermission(permission Permission) {
	for i, p := range u.permissions {
		if p == permission {
			u.permissions = append(u.permissions[:i], u.permissions[i+1:]...)
			return
		}
	}
}

// SetEnvFile sets the environment file path
func (u *User) SetEnvFile(envFile string) {
	u.envFile = envFile
}

// UpdateLastUsed updates the last used timestamp
func (u *User) UpdateLastUsed() {
	now := time.Now()
	u.lastUsedAt = &now
}

// Deactivate deactivates the user
func (u *User) Deactivate() {
	u.status = UserStatusInactive
}

// Activate activates the user
func (u *User) Activate() {
	u.status = UserStatusActive
}

// HasPermission checks if the user has a specific permission
func (u *User) HasPermission(permission Permission) bool {
	// Admin has all permissions
	for _, role := range u.roles {
		if role == RoleAdmin {
			return true
		}
	}

	// Check for wildcard permission
	for _, p := range u.permissions {
		if p == "*" {
			return true
		}
		if p == permission {
			return true
		}
	}

	return false
}

// HasAnyPermission checks if the user has any of the specified permissions
func (u *User) HasAnyPermission(permissions ...Permission) bool {
	for _, permission := range permissions {
		if u.HasPermission(permission) {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the user is an admin
func (u *User) IsAdmin() bool {
	for _, role := range u.roles {
		if role == RoleAdmin {
			return true
		}
	}
	return false
}

// CanAccessSession checks if the user can access a specific session
func (u *User) CanAccessSession(sessionUserID UserID) bool {
	// Admin can access all sessions
	if u.IsAdmin() {
		return true
	}

	// Users can only access their own sessions
	return u.id == sessionUserID
}

// IsMemberOfTeam checks if the user is a member of the specified team
// teamID must be in the format "org/team-slug" (slash-separated)
func (u *User) IsMemberOfTeam(teamID string) bool {
	// Service accounts are associated with a specific team via teamID field
	if u.userType == UserTypeServiceAccount {
		return u.teamID == teamID
	}
	if u.teamsResolved {
		for _, resolvedTeamID := range u.resolvedTeamIDs {
			if resolvedTeamID == teamID {
				return true
			}
		}
		return false
	}

	if u.githubInfo == nil {
		return false
	}

	for _, team := range u.githubInfo.Teams() {
		fullTeamID := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		if fullTeamID == teamID {
			return true
		}
	}
	return false
}

// CanAccessResource checks if the user can access a resource based on its scope
// For team-scoped resources, admin or any team member can access
// For user-scoped resources, only the owner can access (admin privileges do not apply)
func (u *User) CanAccessResource(ownerUserID UserID, scope string, teamID string) bool {
	// Team-scoped: admin or team member can access
	if scope == "team" && teamID != "" {
		if u.IsAdmin() {
			return true
		}
		return u.IsMemberOfTeam(teamID)
	}

	// User-scoped: only the owner can access, regardless of admin status
	// Admin privileges do not extend to other users' personal resources
	return u.id == ownerUserID
}

// UpdateGitHubInfo updates GitHub-specific information
func (u *User) UpdateGitHubInfo(githubInfo *GitHubUserInfo) error {
	if u.userType != UserTypeGitHub {
		return errors.New("cannot update GitHub info for non-GitHub user")
	}
	u.githubInfo = githubInfo
	return nil
}

// SetGitHubInfo sets GitHub information for the user
func (u *User) SetGitHubInfo(info *GitHubUserInfo, teams []GitHubTeamMembership) {
	u.githubInfo = info
	if info != nil {
		u.githubInfo.teams = teams
	}
}

// GetDisplayName returns a display-friendly name
func (u *User) GetDisplayName() string {
	if u.githubInfo != nil && u.githubInfo.Name() != "" {
		return u.githubInfo.Name()
	}
	if u.username != "" {
		return u.username
	}
	return string(u.id)
}

// Validate ensures the user is in a valid state
func (u *User) Validate() error {
	if u.id == "" {
		return errors.New("user ID cannot be empty")
	}

	if u.username == "" {
		return errors.New("username cannot be empty")
	}

	if u.userType == "" {
		return errors.New("user type cannot be empty")
	}

	// Validate user type
	validTypes := []UserType{UserTypeAPIKey, UserTypeGitHub, UserTypeServiceAccount}
	typeValid := false
	for _, validType := range validTypes {
		if u.userType == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid user type: %s", u.userType)
	}

	// Validate roles
	validRoles := []Role{RoleAdmin, RoleUser, RoleMember, RoleDeveloper, RoleReadOnly}
	for _, role := range u.roles {
		roleValid := false
		for _, validRole := range validRoles {
			if role == validRole {
				roleValid = true
				break
			}
		}
		if !roleValid {
			return fmt.Errorf("invalid role: %s", role)
		}
	}

	// Validate email format if provided
	if u.email != nil && !strings.Contains(*u.email, "@") {
		return fmt.Errorf("invalid email format: %s", *u.email)
	}

	if u.createdAt.IsZero() {
		return errors.New("created at time cannot be zero")
	}

	return nil
}
