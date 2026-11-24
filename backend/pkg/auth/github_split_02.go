package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func (p *GitHubAuthProvider) getUserTeamsWithWildcard(ctx context.Context, token string) ([]GitHubTeamMembership, error) {
	// Get all teams user belongs to
	allTeams, err := p.getAllUserTeams(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}

	// Get all configured patterns
	patterns := p.configuredTeamPatterns()

	log.Printf("[AUTH_DEBUG] Configured patterns: %v", patterns)

	// Filter teams that match any configured pattern
	var matchedTeams []GitHubTeamMembership
	for _, team := range allTeams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)

		// Check each pattern for a match
		for _, pattern := range patterns {
			if matchTeamPattern(pattern, team.Organization, team.TeamSlug) {
				matchedTeams = append(matchedTeams, team)
				log.Printf("[AUTH_DEBUG] Pattern match: %s matched %s", pattern, teamKey)
				break // Only add the team once even if multiple patterns match
			}
		}
	}

	log.Printf("[AUTH_DEBUG] Found %d matching teams via /user/teams", len(matchedTeams))
	return matchedTeams, nil
}

// getUserTeamsExactMatch retrieves only exact-match configured team memberships from GitHub API
func (p *GitHubAuthProvider) getUserTeamsExactMatch(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// Extract unique organizations from configured team mappings
	configuredOrgs := make(map[string][]string) // org -> []teamSlugs
	for _, teamKey := range p.configuredTeamPatterns() {
		if strings.Contains(teamKey, "*") {
			continue
		}
		parts := strings.Split(teamKey, "/")
		if len(parts) == 2 {
			org, teamSlug := parts[0], parts[1]
			configuredOrgs[org] = append(configuredOrgs[org], teamSlug)
		}
	}

	if len(configuredOrgs) == 0 {
		log.Printf("[AUTH_DEBUG] No configured team mappings found, returning empty teams")
		return []GitHubTeamMembership{}, nil
	}

	log.Printf("[AUTH_DEBUG] Checking %d configured organizations: %v", len(configuredOrgs), configuredOrgs)

	// Use buffered channel to limit concurrent requests
	maxConcurrent := 3
	semaphore := make(chan struct{}, maxConcurrent)

	type teamResult struct {
		team GitHubTeamMembership
		err  error
	}

	var wg sync.WaitGroup
	resultChan := make(chan teamResult, 100) // Large buffer for potential teams

	// Check membership for each configured team concurrently
	for org, teamSlugs := range configuredOrgs {
		for _, teamSlug := range teamSlugs {
			wg.Add(1)
			go func(orgName, slug string) {
				defer wg.Done()

				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				if isMember, role := p.checkTeamMembership(ctx, token, orgName, slug, username); isMember {
					// Get team name (can be optimized further with batch API if needed)
					teamName := slug // Default to slug if name retrieval fails

					resultChan <- teamResult{
						team: GitHubTeamMembership{
							Organization: orgName,
							TeamSlug:     slug,
							TeamName:     teamName,
							Role:         role,
						},
						err: nil,
					}
				}
			}(org, teamSlug)
		}
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var userTeams []GitHubTeamMembership
	for result := range resultChan {
		if result.err == nil {
			userTeams = append(userTeams, result.team)
		}
	}

	log.Printf("[AUTH_DEBUG] Found %d matching teams for user %s", len(userTeams), username)
	return userTeams, nil
}

func mergeTeamMemberships(groups ...[]GitHubTeamMembership) []GitHubTeamMembership {
	merged := make([]GitHubTeamMembership, 0)
	seen := make(map[string]struct{})
	for _, teams := range groups {
		for _, team := range teams {
			key := strings.ToLower(team.Organization + "/" + team.TeamSlug)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, team)
		}
	}
	return merged
}

// checkTeamMembership checks if user is a member of a specific team without caching
func (p *GitHubAuthProvider) checkTeamMembership(ctx context.Context, token, org, teamSlug, username string) (bool, string) {
	url := fmt.Sprintf("%s/orgs/%s/teams/%s/memberships/%s",
		strings.TrimSuffix(p.config.BaseURL, "/"), org, teamSlug, username)

	log.Printf("[AUTH_DEBUG] Checking team membership: org=%s, team=%s, user=%s", org, teamSlug, username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("[AUTH_DEBUG] Failed to create request: %v", err)
		return false, ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("[AUTH_DEBUG] GitHub API request failed: %v", err)
		return false, ""
	}
	defer utils.SafeCloseResponse(resp)

	log.Printf("[AUTH_DEBUG] GitHub API response status: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[AUTH_DEBUG] Team membership not found (404) for %s in %s/%s", username, org, teamSlug)
		return false, ""
	}

	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		log.Printf("[AUTH_DEBUG] HTTP response check failed: %v", err)
		return false, ""
	}

	var membership struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		log.Printf("[AUTH_DEBUG] Failed to decode membership response: %v", err)
		return false, ""
	}

	log.Printf("[AUTH_DEBUG] Team membership response: state=%s, role=%s", membership.State, membership.Role)
	return membership.State == "active", membership.Role
}

// getUserOrganizations retrieves user's organizations from GitHub API without caching
func (p *GitHubAuthProvider) getUserOrganizations(ctx context.Context, token string) ([]GitHubOrganization, error) {
	url := fmt.Sprintf("%s/user/orgs", strings.TrimSuffix(p.config.BaseURL, "/"))

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

	var orgs []GitHubOrganization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

// mapUserPermissions maps user's team memberships to roles and permissions
// Returns: role, permissions, envFile
func (p *GitHubAuthProvider) mapUserPermissions(teams []GitHubTeamMembership) (string, []string, string) {
	log.Printf("[AUTH_DEBUG] Starting mapUserPermissions")
	log.Printf("[AUTH_DEBUG] Default role: %s", p.config.UserMapping.DefaultRole)
	log.Printf("[AUTH_DEBUG] Default permissions: %v", p.config.UserMapping.DefaultPermissions)
	log.Printf("[AUTH_DEBUG] Team role mappings: %+v", p.config.UserMapping.TeamRoleMapping)
	log.Printf("[AUTH_DEBUG] User teams: %+v", teams)

	highestRole := p.config.UserMapping.DefaultRole
	allPermissions := make(map[string]bool)
	envFile := "" // Track the env file for the highest priority team

	// Add default permissions
	for _, perm := range p.config.UserMapping.DefaultPermissions {
		allPermissions[perm] = true
		log.Printf("[AUTH_DEBUG] Added default permission: %s", perm)
	}

	// Check each team membership against configured rules
	for _, team := range teams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		log.Printf("[AUTH_DEBUG] Checking team: %s", teamKey)

		matchFound := false

		// Check all configured patterns for matches
		for pattern, rule := range p.config.UserMapping.TeamRoleMapping {
			if matchTeamPattern(pattern, team.Organization, team.TeamSlug) {
				matchFound = true
				log.Printf("[AUTH_DEBUG] Found matching rule for %s (pattern: %s): role=%s, permissions=%v", teamKey, pattern, rule.Role, rule.Permissions)

				// Apply higher role if found
				if p.isHigherRole(rule.Role, highestRole) {
					log.Printf("[AUTH_DEBUG] Upgrading role from %s to %s", highestRole, rule.Role)
					highestRole = rule.Role
					// Update env file to match the highest role's team
					if rule.EnvFile != "" {
						envFile = rule.EnvFile
						log.Printf("[AUTH_DEBUG] Setting env file to: %s", envFile)
					}
				}

				// Add permissions from this rule
				for _, perm := range rule.Permissions {
					allPermissions[perm] = true
					log.Printf("[AUTH_DEBUG] Added team permission: %s", perm)
				}
			}
		}

		if !matchFound {
			log.Printf("[AUTH_DEBUG] No rule found for team: %s", teamKey)
			log.Printf("[AUTH_DEBUG] Available team mappings:")
			for availableKey := range p.config.UserMapping.TeamRoleMapping {
				log.Printf("[AUTH_DEBUG]   - %s", availableKey)
			}
		}
	}

	// Convert permissions map to slice
	permissions := make([]string, 0, len(allPermissions))
	for perm := range allPermissions {
		permissions = append(permissions, perm)
	}

	log.Printf("[AUTH_DEBUG] Final role: %s, permissions: %v, envFile: %s", highestRole, permissions, envFile)
	return highestRole, permissions, envFile
}

// isHigherRole checks if role1 has higher priority than role2
func (p *GitHubAuthProvider) isHigherRole(role1, role2 string) bool {
	rolePriority := map[string]int{
		"guest":     0,
		"user":      1,
		"member":    2,
		"developer": 3,
		"admin":     4,
	}

	priority1, exists1 := rolePriority[role1]
	priority2, exists2 := rolePriority[role2]

	if !exists1 || !exists2 {
		return false
	}

	return priority1 > priority2
}

// ExtractTokenFromHeader extracts GitHub token from Authorization header
func ExtractTokenFromHeader(header string) string {
	if header == "" {
		return ""
	}

	// Handle "Bearer <token>" format
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}

	// Handle "token <token>" format
	if strings.HasPrefix(header, "token ") {
		return strings.TrimPrefix(header, "token ")
	}

	// Handle raw token
	return header
}

// matchWildcard matches a string against a pattern with wildcard support
// Supported patterns:
//   - "exact" - exact match
//   - "*" - matches any string
//   - "prefix-*" - matches strings starting with "prefix-"
//   - "*-suffix" - matches strings ending with "-suffix"
func matchWildcard(pattern, str string) bool {
	// Exact match for "*"
	if pattern == "*" {
		return true
	}

	// No wildcard - exact match required
	if !strings.Contains(pattern, "*") {
		return pattern == str
	}

	// Prefix pattern: "prefix-*"
	if strings.HasSuffix(pattern, "-*") {
		prefix := strings.TrimSuffix(pattern, "-*")
		return strings.HasPrefix(str, prefix+"-")
	}

	// Suffix pattern: "*-suffix"
	if strings.HasPrefix(pattern, "*-") {
		suffix := strings.TrimPrefix(pattern, "*-")
		return strings.HasSuffix(str, "-"+suffix)
	}

	// If we reach here, the pattern has * in an unsupported position
	return false
}

// matchTeamPattern matches a team against a pattern
// Pattern format: "org/team" where both org and team can contain wildcards
// Examples:
//   - "myorg/myteam" - exact match
//   - "*/myteam" - any org, exact team name
//   - "myorg/*" - exact org, any team
//   - "myorg/*-engineer" - exact org, team ending with "-engineer"
//   - "myorg/backend-*" - exact org, team starting with "backend-"
