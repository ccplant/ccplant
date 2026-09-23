package controllers

import (
	"errors"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func assertHTTPError(t *testing.T, err error, expectedCode int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *echo.HTTPError
	require.True(t, errors.As(err, &httpErr), "expected *echo.HTTPError, got %T: %v", err, err)
	assert.Equal(t, expectedCode, httpErr.Code)
}

func newTestAPIKeyUser(userID string) *entities.User {
	return entities.NewUser(entities.UserID(userID), entities.UserTypeAPIKey, userID)
}

func newTestGitHubUser(userID, org, teamSlug string) *entities.User {
	info := entities.NewGitHubUserInfo(1, userID, userID, "", "", "", "")
	user := entities.NewGitHubUser(entities.UserID(userID), userID, "", info)
	user.SetGitHubInfo(info, []entities.GitHubTeamMembership{{Organization: org, TeamSlug: teamSlug, TeamName: teamSlug, Role: "member"}})
	return user
}

func newTestAdminUser(userID string) *entities.User {
	user := entities.NewUser(entities.UserID(userID), entities.UserTypeAdmin, userID)
	_ = user.SetRoles([]entities.Role{entities.RoleAdmin})
	user.SetPermissions([]entities.Permission{entities.PermissionAdmin})
	return user
}
