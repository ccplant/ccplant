package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestGetUserInfoIncludesPrincipalID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/user/info", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	user := entities.NewGitHubUser(
		entities.UserID("6f87bb4e-a1e6-4df9-92fe-40f18bde430d"),
		"alice",
		"alice@example.com",
		entities.NewGitHubUserInfo(1, "alice", "Alice", "alice@example.com", "", "", ""),
	)
	ctx.Set("internal_user", user)

	require.NoError(t, NewUserController().GetUserInfo(ctx))
	require.Equal(t, http.StatusOK, rec.Code)

	var response UserInfoResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, string(user.ID()), response.PrincipalID)
	require.Equal(t, "alice", response.Username)
}

func TestOwnerAuthorizationDoesNotNormalizePrincipalID(t *testing.T) {
	principalID := "acme-platform"
	user := entities.NewGitHubUser(
		entities.UserID(principalID),
		"alice",
		"alice@example.com",
		entities.NewGitHubUserInfo(1, "alice", "Alice", "alice@example.com", "", "", ""),
	)
	user.SetGitHubInfo(user.GitHubInfo(), []entities.GitHubTeamMembership{{Organization: "acme", TeamSlug: "platform"}})

	credentials := NewCredentialsController(nil)
	settings := NewSettingsController(nil, nil)
	require.True(t, credentials.canAccess(user, principalID))
	require.True(t, credentials.canModify(user, principalID))
	require.True(t, settings.canAccess(user, principalID))
	require.True(t, settings.canModify(user, principalID))
	require.True(t, credentials.canAccess(user, "acme/platform"))
	require.True(t, settings.canAccess(user, "acme/platform"))

	// A principal that normalizes to the team name must not gain team access.
	user.SetGitHubInfo(user.GitHubInfo(), nil)
	require.False(t, credentials.canAccess(user, "acme/platform"))
	require.False(t, credentials.canModify(user, "acme/platform"))
	require.False(t, settings.canAccess(user, "acme/platform"))
	require.False(t, settings.canModify(user, "acme/platform"))
}
