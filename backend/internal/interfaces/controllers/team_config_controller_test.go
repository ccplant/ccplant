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

func TestTeamConfigControllerCreateRequiresAuthentication(t *testing.T) {
	controller := NewTeamConfigController(&teamConfigControllerRepo{teams: map[string]*entities.TeamConfig{}})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"name":"platform"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := e.NewContext(req, httptest.NewRecorder())
	ctx.Set("authz_context", &auth.AuthorizationContext{TeamScope: auth.TeamScopeAuth{}})

	err := controller.Create(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusForbidden, httpErr.Code)
}

func TestTeamConfigControllerCreateAndListAsRegularUser(t *testing.T) {
	repo := &teamConfigControllerRepo{teams: map[string]*entities.TeamConfig{}}
	controller := NewTeamConfigController(repo)
	e := echo.New()
	authz := &auth.AuthorizationContext{User: entities.NewUser("user-1", entities.UserTypeRegular, "alice")}

	req := httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"name":"Platform / コア"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(req, recorder)
	ctx.Set("authz_context", authz)
	require.NoError(t, controller.Create(ctx))
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"team_id":"team-`)
	require.Contains(t, recorder.Body.String(), `"principal_id":"team-`)
	require.Contains(t, recorder.Body.String(), `"name":"Platform / コア"`)

	listRecorder := httptest.NewRecorder()
	listCtx := e.NewContext(httptest.NewRequest(http.MethodGet, "/teams", nil), listRecorder)
	listCtx.Set("authz_context", authz)
	require.NoError(t, controller.List(listCtx))
	require.Contains(t, listRecorder.Body.String(), `"name":"Platform / コア"`)

	duplicateRecorder := httptest.NewRecorder()
	duplicateCtx := e.NewContext(httptest.NewRequest(http.MethodPost, "/teams", bytes.NewBufferString(`{"name":"Platform / コア"}`)), duplicateRecorder)
	duplicateCtx.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	duplicateCtx.Set("authz_context", authz)
	require.NoError(t, controller.Create(duplicateCtx))
	require.Equal(t, http.StatusCreated, duplicateRecorder.Code)
	require.Len(t, repo.teams, 2)
}

func TestTeamConfigControllerRenameAndDelete(t *testing.T) {
	team := entities.NewTeamConfig("team-01ARZ3NDEKTSV4RRFFQ69G5FAV", nil, nil)
	team.SetPrincipalID(team.TeamID())
	team.SetName("Before")
	team.SetOwnerIDs([]string{"user-1"})
	repo := &teamConfigControllerRepo{teams: map[string]*entities.TeamConfig{team.TeamID(): team}}
	controller := NewTeamConfigController(repo)
	e := echo.New()
	authz := &auth.AuthorizationContext{User: entities.NewUser("user-1", entities.UserTypeRegular, "alice")}

	renameRecorder := httptest.NewRecorder()
	renameCtx := e.NewContext(httptest.NewRequest(http.MethodPatch, "/teams/"+team.TeamID(), bytes.NewBufferString(`{"name":"After"}`)), renameRecorder)
	renameCtx.SetParamNames("team")
	renameCtx.SetParamValues(team.TeamID())
	renameCtx.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	renameCtx.Set("authz_context", authz)
	require.NoError(t, controller.Rename(renameCtx))
	require.Equal(t, "After", team.Name())

	deleteRecorder := httptest.NewRecorder()
	deleteCtx := e.NewContext(httptest.NewRequest(http.MethodDelete, "/teams/"+team.TeamID(), nil), deleteRecorder)
	deleteCtx.SetParamNames("team")
	deleteCtx.SetParamValues(team.TeamID())
	deleteCtx.Set("authz_context", authz)
	require.NoError(t, controller.Delete(deleteCtx))
	require.Equal(t, http.StatusNoContent, deleteRecorder.Code)
	require.Empty(t, repo.teams)
}
