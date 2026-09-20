package controllers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type teamConfigControllerRepo struct {
	teams map[string]*entities.TeamConfig
}

func (r *teamConfigControllerRepo) Save(_ context.Context, team *entities.TeamConfig) error {
	if team.PrincipalID() == "" {
		principalID, err := entities.NewTeamPrincipalID()
		if err != nil {
			return err
		}
		team.SetPrincipalID(principalID)
	}
	r.teams[team.TeamID()] = team
	return nil
}

func (r *teamConfigControllerRepo) FindByTeamID(_ context.Context, teamID string) (*entities.TeamConfig, error) {
	team, ok := r.teams[teamID]
	if !ok {
		return nil, errors.New("not found")
	}
	return team, nil
}

func (r *teamConfigControllerRepo) Delete(_ context.Context, teamID string) error {
	delete(r.teams, teamID)
	return nil
}

func (r *teamConfigControllerRepo) Exists(_ context.Context, teamID string) (bool, error) {
	_, ok := r.teams[teamID]
	return ok, nil
}

func (r *teamConfigControllerRepo) List(_ context.Context) ([]*entities.TeamConfig, error) {
	teams := make([]*entities.TeamConfig, 0, len(r.teams))
	for _, team := range r.teams {
		teams = append(teams, team)
	}
	return teams, nil
}

func TestTeamConfigControllerCreateRequiresAdmin(t *testing.T) {
	controller := NewTeamConfigController(&teamConfigControllerRepo{teams: map[string]*entities.TeamConfig{}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"team_id":"platform"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.Set("authz_context", &auth.AuthorizationContext{TeamScope: auth.TeamScopeAuth{}})

	err := controller.Create(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusForbidden, httpErr.Code)
}

func TestTeamConfigControllerCreateAndList(t *testing.T) {
	repo := &teamConfigControllerRepo{teams: map[string]*entities.TeamConfig{}}
	controller := NewTeamConfigController(repo)
	e := echo.New()
	authz := &auth.AuthorizationContext{TeamScope: auth.TeamScopeAuth{IsAdmin: true}}

	req := httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"team_id":"Platform/Core"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(req, recorder)
	ctx.Set("authz_context", authz)
	require.NoError(t, controller.Create(ctx))
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"team_id":"platform/core"`)
	require.Contains(t, recorder.Body.String(), `"principal_id":"team-`)

	listRecorder := httptest.NewRecorder()
	listCtx := e.NewContext(httptest.NewRequest(http.MethodGet, "/teams", nil), listRecorder)
	listCtx.Set("authz_context", authz)
	require.NoError(t, controller.List(listCtx))
	require.Contains(t, listRecorder.Body.String(), `"team_id":"platform/core"`)

	duplicateCtx := e.NewContext(httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"team_id":"platform/core"}`)), httptest.NewRecorder())
	duplicateCtx.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	duplicateCtx.Set("authz_context", authz)
	err := controller.Create(duplicateCtx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusConflict, httpErr.Code)
}
