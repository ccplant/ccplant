package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type personalUseProfileRepo struct {
	profiles []*entities.SessionProfile
	filters  []repositories.SessionProfileFilter
}

func (r *personalUseProfileRepo) Create(context.Context, *entities.SessionProfile) error { return nil }
func (r *personalUseProfileRepo) Get(_ context.Context, id string) (*entities.SessionProfile, error) {
	for _, profile := range r.profiles {
		if profile.ID() == id {
			return profile, nil
		}
	}
	return nil, entities.ErrSessionProfileNotFound{ID: id}
}
func (r *personalUseProfileRepo) List(_ context.Context, filter repositories.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	r.filters = append(r.filters, filter)
	result := make([]*entities.SessionProfile, 0)
	for _, profile := range r.profiles {
		if filter.Scope != "" && profile.Scope() != filter.Scope {
			continue
		}
		if filter.UserID != "" && profile.UserID() != filter.UserID {
			continue
		}
		if len(filter.TeamIDs) > 0 && profile.Scope() == entities.ScopeTeam {
			allowed := false
			for _, teamID := range filter.TeamIDs {
				allowed = allowed || profile.TeamID() == teamID
			}
			if !allowed {
				continue
			}
		}
		result = append(result, profile)
	}
	return result, nil
}
func (r *personalUseProfileRepo) Update(context.Context, *entities.SessionProfile) error { return nil }
func (r *personalUseProfileRepo) Delete(context.Context, string) error                   { return nil }

func TestListPersonalSessionProfilesIncludesProfilesFromUserTeams(t *testing.T) {
	personal := entities.NewSessionProfile("personal", "Personal", "alice")
	team := entities.NewSessionProfile("team", "Team", "owner")
	team.SetScope(entities.ScopeTeam)
	team.SetTeamID("acme/platform")
	outside := entities.NewSessionProfile("outside", "Outside", "owner")
	outside.SetScope(entities.ScopeTeam)
	outside.SetTeamID("other/team")
	repo := &personalUseProfileRepo{profiles: []*entities.SessionProfile{personal, team, outside}}
	controller := NewSessionProfileController(repo, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/session-profiles?scope=user", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	user := entities.NewUser("alice", entities.UserTypeRegular, "alice")
	ctx.Set("internal_user", user)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User: user, PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanRead: true},
		TeamScope: auth.TeamScopeAuth{Teams: []string{"acme/platform"}},
	})

	require.NoError(t, controller.ListSessionProfiles(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		Profiles []SessionProfileResponse `json:"session_profiles"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Profiles, 2)
	require.ElementsMatch(t, []string{"personal", "team"}, []string{response.Profiles[0].ID, response.Profiles[1].ID})
}

func TestCanUseTeamProfileFromPersonalScopeRequiresMembership(t *testing.T) {
	profile := entities.NewSessionProfile("team", "Team", "owner")
	profile.SetScope(entities.ScopeTeam)
	profile.SetTeamID("acme/platform")
	controller := &SessionController{}

	member := &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice"},
		TeamScope:     auth.TeamScopeAuth{Teams: []string{"acme/platform"}},
	}
	require.True(t, controller.canUseSessionProfile(member, profile, entities.ScopeUser, ""))
	require.False(t, controller.canUseSessionProfile(member, profile, entities.ScopeTeam, "other/team"))
	require.False(t, controller.canUseSessionProfile(&auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice"},
	}, profile, entities.ScopeUser, ""))
}
