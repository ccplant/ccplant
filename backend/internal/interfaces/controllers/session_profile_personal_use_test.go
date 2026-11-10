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

type personalUseSettingsRepo struct{ settings *entities.Settings }

func (r personalUseSettingsRepo) Save(context.Context, *entities.Settings) error { return nil }
func (r personalUseSettingsRepo) FindByName(context.Context, string) (*entities.Settings, error) {
	return r.settings, nil
}
func (r personalUseSettingsRepo) Delete(context.Context, string) error { return nil }
func (r personalUseSettingsRepo) Exists(context.Context, string) (bool, error) {
	return r.settings != nil, nil
}
func (r personalUseSettingsRepo) List(context.Context) ([]*entities.Settings, error) { return nil, nil }

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
	settings := entities.NewSettings("alice")
	settings.SetEnabledTeamSessionProfileIDs([]string{"team"})
	controller := NewSessionProfileController(repo, personalUseSettingsRepo{settings: settings})

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
	require.True(t, response.Profiles[1].PersonalEnabled)
}

func TestListAvailableTeamProfilesMarksPersonalOptIn(t *testing.T) {
	teamA := entities.NewSessionProfile("team-a", "Team A", "owner")
	teamA.SetScope(entities.ScopeTeam)
	teamA.SetTeamID("acme/platform")
	teamB := entities.NewSessionProfile("team-b", "Team B", "owner")
	teamB.SetScope(entities.ScopeTeam)
	teamB.SetTeamID("acme/platform")
	settings := entities.NewSettings("alice")
	settings.SetEnabledTeamSessionProfileIDs([]string{"team-a"})
	controller := NewSessionProfileController(
		&personalUseProfileRepo{profiles: []*entities.SessionProfile{teamA, teamB}},
		personalUseSettingsRepo{settings: settings},
	)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/session-profiles?scope=user&include_available_team_profiles=true", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	user := entities.NewUser("alice", entities.UserTypeRegular, "alice")
	ctx.Set("internal_user", user)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User: user, PersonalScope: auth.PersonalScopeAuth{UserID: "alice", CanRead: true},
		TeamScope: auth.TeamScopeAuth{Teams: []string{"acme/platform"}},
	})

	require.NoError(t, controller.ListSessionProfiles(ctx))
	var response struct {
		Profiles []SessionProfileResponse `json:"session_profiles"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Profiles, 2)
	require.True(t, response.Profiles[0].PersonalEnabled)
	require.False(t, response.Profiles[1].PersonalEnabled)
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

func TestResolveSessionProfileIncludesEnabledTeamProfileInAutomaticSelection(t *testing.T) {
	team := entities.NewSessionProfile("team", "Team", "owner")
	team.SetScope(entities.ScopeTeam)
	team.SetTeamID("acme/platform")
	team.SetSelectorTags(map[string]string{"repository": "acme/app"})
	settings := entities.NewSettings("alice")
	settings.SetEnabledTeamSessionProfileIDs([]string{"team"})
	controller := &SessionController{
		sessionProfileRepo: &personalUseProfileRepo{profiles: []*entities.SessionProfile{team}},
		settingsRepo:       personalUseSettingsRepo{settings: settings},
	}
	authz := &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "alice"},
		TeamScope:     auth.TeamScopeAuth{Teams: []string{"acme/platform"}},
	}

	profile, reason, err := controller.resolveSessionProfile(
		context.Background(), "", "alice", entities.ScopeUser, "",
		map[string]string{"repository": "acme/app"}, authz,
	)
	require.NoError(t, err)
	require.Equal(t, "team", profile.ID())
	require.Equal(t, "most_specific_selector_tags_match", reason)

	settings.SetEnabledTeamSessionProfileIDs(nil)
	profile, _, err = controller.resolveSessionProfile(
		context.Background(), "", "alice", entities.ScopeUser, "",
		map[string]string{"repository": "acme/app"}, authz,
	)
	require.NoError(t, err)
	require.Nil(t, profile)

	profile, _, err = controller.resolveSessionProfile(
		context.Background(), "team", "alice", entities.ScopeUser, "", nil, authz,
	)
	require.Error(t, err)
	require.Nil(t, profile)
}
