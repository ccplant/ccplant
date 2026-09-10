package services

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// TeamMembershipResolver translates connection-aware GitHub memberships into
// stable ccplant team keys and lazily creates/adopts discovered TeamConfigs.
type TeamMembershipResolver struct {
	repo  repositories.TeamConfigRepository
	rules []config.TeamDiscoveryRule
}

func NewTeamMembershipResolver(repo repositories.TeamConfigRepository, rules []config.TeamDiscoveryRule) *TeamMembershipResolver {
	return &TeamMembershipResolver{repo: repo, rules: append([]config.TeamDiscoveryRule(nil), rules...)}
}

// Resolve returns team keys. resolved is false when no mapping/discovery is
// configured, which preserves the legacy direct GitHub-team behavior.
func (r *TeamMembershipResolver) Resolve(ctx context.Context, memberships []entities.GitHubTeamMembership) ([]string, bool, error) {
	configs, err := r.repo.List(ctx)
	if err != nil {
		return nil, false, err
	}
	configured := len(r.rules) > 0
	for _, team := range configs {
		if len(team.ExternalTeams()) > 0 {
			configured = true
		}
	}
	if !configured {
		return nil, false, nil
	}

	resolved := make(map[string]struct{})
	for _, membership := range memberships {
		connectionID := membership.ConnectionID
		if connectionID == "" {
			connectionID = "github"
		}
		organization := strings.ToLower(strings.TrimSpace(membership.Organization))
		teamSlug := strings.ToLower(strings.TrimSpace(membership.TeamSlug))
		matchedTeamID := ""
		for _, team := range configs {
			if matchesBinding(team.ExternalTeams(), connectionID, organization, teamSlug) {
				if matchedTeamID != "" && matchedTeamID != team.TeamID() {
					return nil, true, fmt.Errorf("external GitHub team %s:%s/%s maps to multiple ccplant teams", connectionID, organization, teamSlug)
				}
				matchedTeamID = team.TeamID()
			}
		}
		if matchedTeamID != "" {
			resolved[matchedTeamID] = struct{}{}
		}

		fullName := organization + "/" + teamSlug
		for _, rule := range r.rules {
			if rule.ConnectionID != connectionID {
				continue
			}
			matched, matchErr := path.Match(strings.ToLower(rule.TeamPattern), fullName)
			if matchErr != nil {
				return nil, true, fmt.Errorf("invalid team discovery pattern %q: %w", rule.TeamPattern, matchErr)
			}
			if !matched {
				continue
			}
			team, ensureErr := r.ensureDiscoveredTeam(ctx, fullName, entities.ExternalTeamBinding{
				ConnectionID: connectionID,
				Organization: organization,
				TeamSlug:     teamSlug,
				ManagedBy:    "discovery",
			})
			if ensureErr != nil {
				return nil, true, ensureErr
			}
			resolved[team.TeamID()] = struct{}{}
		}
	}

	teamIDs := make([]string, 0, len(resolved))
	for teamID := range resolved {
		teamIDs = append(teamIDs, teamID)
	}
	sort.Strings(teamIDs)
	return teamIDs, true, nil
}

func matchesBinding(bindings []entities.ExternalTeamBinding, connectionID, organization, teamSlug string) bool {
	for _, binding := range bindings {
		if binding.ConnectionID == connectionID && strings.EqualFold(binding.Organization, organization) && strings.EqualFold(binding.TeamSlug, teamSlug) {
			return true
		}
	}
	return false
}

func (r *TeamMembershipResolver) ensureDiscoveredTeam(ctx context.Context, teamID string, binding entities.ExternalTeamBinding) (*entities.TeamConfig, error) {
	team, err := r.repo.FindByTeamID(ctx, teamID)
	if err != nil {
		team = entities.NewTeamConfig(teamID, nil, nil)
		team.SetExternalTeams([]entities.ExternalTeamBinding{binding})
		if saveErr := r.repo.Save(ctx, team); saveErr != nil {
			// A concurrent request may have created it first.
			if team, err = r.repo.FindByTeamID(ctx, teamID); err != nil {
				return nil, saveErr
			}
		}
		return team, nil
	}
	if !matchesBinding(team.ExternalTeams(), binding.ConnectionID, binding.Organization, binding.TeamSlug) {
		bindings := team.ExternalTeams()
		bindings = append(bindings, binding)
		team.SetExternalTeams(bindings)
		if err := r.repo.Save(ctx, team); err != nil {
			return nil, err
		}
	}
	return team, nil
}
