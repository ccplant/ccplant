package controllers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestListSecretsDecodesTeamNameSlash(t *testing.T) {
	repo := newMockSettingsRepository()
	require.NoError(t, repo.Save(context.Background(), entities.NewSettings("ccplant/developers")))

	user := createTestUser("member", false)
	user.SetGitHubInfo(&entities.GitHubUserInfo{}, []entities.GitHubTeamMembership{{
		Organization: "ccplant",
		TeamSlug:     "developers",
	}})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/settings/ccplant%2Fdevelopers/secrets", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("name")
	ctx.SetParamValues("ccplant\x01developers")
	ctx.Set("internal_user", user)

	require.NoError(t, NewSettingsController(repo, nil).ListSecrets(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"secrets":[]}`, rec.Body.String())
}
