package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func matchTeamPattern(pattern, org, teamSlug string) bool {
	parts := strings.Split(pattern, "/")
	if len(parts) != 2 {
		return false
	}

	orgPattern, teamPattern := parts[0], parts[1]

	// Check organization match
	if !matchWildcard(orgPattern, org) {
		return false
	}

	// Check team match
	return matchWildcard(teamPattern, teamSlug)
}

// hasWildcardPatterns checks if any team mappings contain wildcard patterns
func (p *GitHubAuthProvider) hasWildcardPatterns() bool {
	for _, teamKey := range p.configuredTeamPatterns() {
		if strings.Contains(teamKey, "*") {
			return true
		}
	}
	return false
}

func (p *GitHubAuthProvider) configuredTeamPatterns() []string {
	patterns := make([]string, 0, len(p.config.UserMapping.TeamRoleMapping)+len(p.config.TeamDiscoveryPatterns))
	seen := make(map[string]struct{})
	for pattern := range p.config.UserMapping.TeamRoleMapping {
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	for _, pattern := range p.config.TeamDiscoveryPatterns {
		if _, ok := seen[pattern]; ok {
			continue
		}
		seen[pattern] = struct{}{}
		patterns = append(patterns, pattern)
	}
	return patterns
}

// getAllUserTeams retrieves all teams the user belongs to using /user/teams API
func (p *GitHubAuthProvider) getAllUserTeams(ctx context.Context, token string) ([]GitHubTeamMembership, error) {
	var allTeams []GitHubTeamMembership
	page := 1
	perPage := 100 // GitHub API max

	for {
		url := fmt.Sprintf("%s/user/teams?per_page=%d&page=%d",
			strings.TrimSuffix(p.config.BaseURL, "/"), perPage, page)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
		req.Header.Set("Accept", "application/vnd.github.v3+json")

		logVerbose("Making GitHub API request: GET %s", url)
		resp, err := p.client.Do(req)
		if err != nil {
			logVerbose("GitHub API request failed: %v", err)
			return nil, err
		}
		defer utils.SafeCloseResponse(resp)

		logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
		if err := utils.CheckHTTPResponse(resp, url); err != nil {
			return nil, err
		}

		var teams []struct {
			ID           int64  `json:"id"`
			Slug         string `json:"slug"`
			Name         string `json:"name"`
			Permission   string `json:"permission"`
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
			return nil, err
		}

		if len(teams) == 0 {
			break
		}

		for _, team := range teams {
			allTeams = append(allTeams, GitHubTeamMembership{
				Organization: team.Organization.Login,
				TeamSlug:     team.Slug,
				TeamName:     team.Name,
				Role:         team.Permission,
			})
		}

		// Check if there are more pages
		if len(teams) < perPage {
			break
		}
		page++
	}

	log.Printf("[AUTH_DEBUG] Retrieved %d total teams for user via /user/teams", len(allTeams))
	return allTeams, nil
}
