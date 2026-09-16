package services

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

type memoryTeamConfigRepository struct {
	teams map[string]*entities.TeamConfig
}

func (r *memoryTeamConfigRepository) Save(_ context.Context, team *entities.TeamConfig) error {
	if team.PrincipalID() == "" {
		id, err := entities.NewTeamPrincipalID()
		if err != nil {
			return err
		}
		team.SetPrincipalID(id)
	}
	r.teams[team.TeamID()] = team
	return nil
}
func (r *memoryTeamConfigRepository) FindByTeamID(_ context.Context, id string) (*entities.TeamConfig, error) {
	team, ok := r.teams[id]
	if !ok {
		return nil, errors.New("not found")
	}
	if team.PrincipalID() == "" {
		principalID, err := entities.NewTeamPrincipalID()
		if err != nil {
			return nil, err
		}
		team.SetPrincipalID(principalID)
	}
	return team, nil
}
func (r *memoryTeamConfigRepository) Delete(_ context.Context, id string) error {
	delete(r.teams, id)
	return nil
}
func (r *memoryTeamConfigRepository) Exists(_ context.Context, id string) (bool, error) {
	_, ok := r.teams[id]
	return ok, nil
}
func (r *memoryTeamConfigRepository) List(_ context.Context) ([]*entities.TeamConfig, error) {
	result := make([]*entities.TeamConfig, 0, len(r.teams))
	for _, team := range r.teams {
		result = append(result, team)
	}
	return result, nil
}

func TestTeamMembershipResolverDiscoversLegacyTeamAndMapsSecondConnection(t *testing.T) {
	legacy := entities.NewTeamConfig("test/cc-users", nil, map[string]string{"KEEP_ME": "yes"})
	repo := &memoryTeamConfigRepository{teams: map[string]*entities.TeamConfig{"test/cc-users": legacy}}
	resolver := NewTeamMembershipResolver(repo, []config.TeamDiscoveryRule{{ConnectionID: "ghes", TeamPattern: "*/cc-users"}})

	teamIDs, resolved, err := resolver.Resolve(context.Background(), []entities.GitHubTeamMembership{{ConnectionID: "ghes", Organization: "test", TeamSlug: "cc-users"}})
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, []string{"test/cc-users"}, teamIDs)
	require.Regexp(t, regexp.MustCompile(`^team-[0-9A-HJKMNP-TV-Z]{26}$`), legacy.PrincipalID())
	require.Equal(t, "yes", legacy.EnvVars()["KEEP_ME"])

	bindings := legacy.ExternalTeams()
	bindings = append(bindings, entities.ExternalTeamBinding{ConnectionID: "ghec", Organization: "myorg", TeamSlug: "test-cc-users", ManagedBy: "api"})
	legacy.SetExternalTeams(bindings)
	require.NoError(t, repo.Save(context.Background(), legacy))

	teamIDs, resolved, err = resolver.Resolve(context.Background(), []entities.GitHubTeamMembership{{ConnectionID: "ghec", Organization: "myorg", TeamSlug: "test-cc-users"}})
	require.NoError(t, err)
	require.True(t, resolved)
	sort.Strings(teamIDs)
	require.Equal(t, []string{"test/cc-users"}, teamIDs)
}
