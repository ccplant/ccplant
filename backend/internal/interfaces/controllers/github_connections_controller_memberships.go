package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *GitHubConnectionsController) FetchExternalTeamMembers(ctx context.Context, actorPrincipalID string, binding entities.ExternalTeamBinding, builtInToken ...string) ([]entities.ExternalTeamMember, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return nil, err
	}
	merged := make(map[int64]entities.ExternalTeamMember)
	merge := func(members []entities.ExternalTeamMember) {
		for _, member := range members {
			if current, ok := merged[member.GitHubUserID]; ok {
				current.Sources = append(current.Sources, member.Sources...)
				merged[member.GitHubUserID] = current
			} else {
				member.ConnectionID = ""
				for i := range member.Sources {
					member.Sources[i].ConnectionID = ""
				}
				merged[member.GitHubUserID] = member
			}
		}
	}
	for _, identity := range identities {
		if identity.PrincipalID != actorPrincipalID {
			continue
		}
		connection, _, _, err := c.loadConnection(ctx, identity.ConnectionID)
		if err != nil || !connection.Enabled {
			continue
		}
		secret, err := c.client.CoreV1().Secrets(c.namespace).Get(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.GetOptions{})
		if err != nil {
			continue
		}
		if raw := string(secret.Data[githubExpiresAtKey]); raw != "" {
			expiresAt, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil || !expiresAt.After(time.Now().UTC()) {
				continue
			}
		}
		token := string(secret.Data[githubAccessTokenKey])
		if token == "" {
			continue
		}
		members, err := c.fetchGitHubTeamMembers(ctx, connection, token, binding)
		if err == nil {
			merge(members)
		}
	}
	if len(builtInToken) > 0 && builtInToken[0] != "" {
		if members, err := c.fetchGitHubTeamMembers(ctx, c.builtInConnection(), builtInToken[0], binding); err == nil {
			merge(members)
		}
	}
	if len(merged) > 0 {
		result := make([]entities.ExternalTeamMember, 0, len(merged))
		for _, member := range merged {
			result = append(result, member)
		}
		return result, nil
	}
	return nil, fmt.Errorf("no linked GitHub credential can read %s/%s", binding.Organization, binding.TeamSlug)
}

func (c *GitHubConnectionsController) fetchGitHubTeamMembers(ctx context.Context, connection githubConnection, token string, binding entities.ExternalTeamBinding) ([]entities.ExternalTeamMember, error) {
	result := make([]entities.ExternalTeamMember, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/orgs/%s/teams/%s/members?role=all&per_page=100&page=%d", strings.TrimSuffix(connection.APIURL, "/"), url.PathEscape(binding.Organization), url.PathEscape(binding.TeamSlug), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		var users []githubOAuthUser
		decodeErr := json.NewDecoder(resp.Body).Decode(&users)
		utils.SafeCloseResponseBody(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub team member lookup returned status %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		for _, user := range users {
			result = append(result, entities.ExternalTeamMember{ConnectionID: connection.ID, GitHubUserID: user.ID, Login: user.Login, Sources: []entities.ExternalTeamRef{{ConnectionID: connection.ID, Organization: strings.ToLower(binding.Organization), TeamSlug: strings.ToLower(binding.TeamSlug)}}})
		}
		if len(users) < 100 {
			return result, nil
		}
	}
}

// ResolveTeamMemberships loads and merges memberships for every GitHub identity
// linked to a principal. Connection IDs are retained as provenance, but are not
// part of the logical team identity used by ccplant.
func (c *GitHubConnectionsController) ResolveTeamMemberships(ctx context.Context, principalID string) ([]auth.GitHubTeamMembership, bool, error) {
	if c.membershipRepo == nil {
		return c.ResolveLiveTeamMemberships(ctx, principalID)
	}
	snapshots, err := c.membershipRepo.ListForPrincipal(ctx, principalID)
	if err != nil {
		return nil, false, err
	}
	result := make([]auth.GitHubTeamMembership, 0)
	seen := make(map[string]struct{})
	for _, snapshot := range snapshots {
		for _, member := range snapshot.Members {
			if member.PrincipalID != principalID {
				continue
			}
			for _, identity := range member.Sources {
				if identity.ConnectionID != "" {
					var linked githubIdentity
					if _, err := c.loadObject(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), &linked); err != nil || linked.PrincipalID != principalID {
						continue
					}
				}
				for _, external := range snapshot.ExternalMembers {
					if identity.ConnectionID != external.ConnectionID || identity.GitHubUserID != external.GitHubUserID {
						continue
					}
					for _, source := range external.Sources {
						key := strings.ToLower(source.Organization) + "\x00" + strings.ToLower(source.TeamSlug)
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
						result = append(result, auth.GitHubTeamMembership{ConnectionID: source.ConnectionID, Organization: source.Organization, TeamSlug: source.TeamSlug})
					}
				}
			}
		}
	}
	// An empty result cannot distinguish a synchronized user with no teams from
	// a principal that predates durable snapshots. Report it as unresolved so
	// authentication paths preserve the live memberships they just fetched.
	return result, len(result) > 0, nil
}

// ResolveTeamMembershipsForGitHubUser maps a built-in OAuth identity to its
// existing stable principal before reading the durable snapshot.
func (c *GitHubConnectionsController) ResolveTeamMembershipsForGitHubUser(ctx context.Context, githubUserID int64) ([]auth.GitHubTeamMembership, bool, error) {
	principal, err := c.loadPrincipal(ctx, fmt.Sprintf("github:%d", githubUserID))
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	memberships, resolved, err := c.ResolveTeamMemberships(ctx, principal.ID)
	if err != nil || !resolved {
		return memberships, resolved, err
	}
	// Existing built-in OAuth principals can predate durable membership
	// snapshots. Treat an empty persisted result as unresolved so the teams
	// already fetched by GitHubAuthProvider are preserved during migration.
	// Once synchronization records memberships, the durable result takes over.
	if len(memberships) == 0 {
		return nil, false, nil
	}
	return memberships, true, nil
}

// ResolveLiveTeamMemberships queries GitHub and is reserved for identity
// creation/link lifecycle synchronization. Normal authentication uses the
// durable snapshots in ResolveTeamMemberships.
func (c *GitHubConnectionsController) ResolveLiveTeamMemberships(ctx context.Context, principalID string) ([]auth.GitHubTeamMembership, bool, error) {
	identities, err := c.listIdentities(ctx)
	if err != nil {
		return nil, false, err
	}
	memberships := make([]auth.GitHubTeamMembership, 0)
	seen := make(map[string]struct{})
	linked := false
	for _, identity := range identities {
		if identity.PrincipalID != principalID {
			continue
		}
		linked = true
		connection, _, _, loadErr := c.loadConnection(ctx, identity.ConnectionID)
		if loadErr != nil || !connection.Enabled {
			log.Printf("[GITHUB_MEMBERSHIP] Skipping unavailable connection %q for principal %q: %v", identity.ConnectionID, principalID, loadErr)
			continue
		}
		secret, secretErr := c.client.CoreV1().Secrets(c.namespace).Get(ctx, identitySecretName(identity.ConnectionID, identity.GitHubUserID), metav1.GetOptions{})
		if secretErr != nil {
			log.Printf("[GITHUB_MEMBERSHIP] Skipping identity %q: %v", identity.ID, secretErr)
			continue
		}
		if raw := string(secret.Data[githubExpiresAtKey]); raw != "" {
			expiresAt, parseErr := time.Parse(time.RFC3339, raw)
			if parseErr != nil || !expiresAt.After(time.Now().UTC()) {
				log.Printf("[GITHUB_MEMBERSHIP] Skipping expired identity %q", identity.ID)
				continue
			}
		}
		token := string(secret.Data[githubAccessTokenKey])
		if token == "" {
			continue
		}
		teams, fetchErr := c.fetchGitHubTeams(ctx, connection, token)
		if fetchErr != nil {
			log.Printf("[GITHUB_MEMBERSHIP] Failed to load teams from connection %q: %v", identity.ConnectionID, fetchErr)
			continue
		}
		for _, team := range teams {
			key := strings.ToLower(strings.TrimSpace(team.Organization)) + "\x00" + strings.ToLower(strings.TrimSpace(team.TeamSlug))
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			team.ConnectionID = identity.ConnectionID
			memberships = append(memberships, team)
		}
	}
	return memberships, linked, nil
}

func (c *GitHubConnectionsController) FetchBuiltInTeams(ctx context.Context, token string) ([]auth.GitHubTeamMembership, error) {
	return c.fetchGitHubTeams(ctx, c.builtInConnection(), token)
}

func (c *GitHubConnectionsController) fetchGitHubTeams(ctx context.Context, connection githubConnection, token string) ([]auth.GitHubTeamMembership, error) {
	result := make([]auth.GitHubTeamMembership, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/user/teams?per_page=100&page=%d", strings.TrimSuffix(connection.APIURL, "/"), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		var teams []struct {
			Slug         string `json:"slug"`
			Name         string `json:"name"`
			Permission   string `json:"permission"`
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&teams)
		utils.SafeCloseResponse(resp)
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("GitHub team endpoint returned %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		for _, team := range teams {
			result = append(result, auth.GitHubTeamMembership{Organization: team.Organization.Login, TeamSlug: team.Slug, TeamName: team.Name, Role: team.Permission})
		}
		if len(teams) < 100 {
			return result, nil
		}
	}
}

// ResolveConnectionURLs returns the runtime web and API endpoints for a
// selected connection. Session workloads must use these endpoints together
// with the selected credential instead of inheriting the deployment-wide
// GitHub Enterprise configuration.
func (c *GitHubConnectionsController) ResolveConnectionURLs(ctx context.Context, connectionID string) (string, string, error) {
	connection, _, _, err := c.loadConnection(ctx, connectionID)
	if err != nil {
		return "", "", fmt.Errorf("load GitHub connection: %w", err)
	}
	if !connection.Enabled {
		return "", "", fmt.Errorf("GitHub connection %q is disabled", connection.Name)
	}
	return connection.BaseURL, connection.APIURL, nil
}

// ResolveAccessTokenForOrganization returns the credential mapped to an organization.
// The boolean is false when no connection mapping exists for the organization.
func (c *GitHubConnectionsController) ResolveAccessTokenForOrganization(ctx context.Context, user *entities.User, organization string) (string, string, bool, error) {
	organization = strings.ToLower(strings.TrimSpace(organization))
	if organization == "" {
		return "", "", false, nil
	}
	connections, err := c.listConnections(ctx)
	if err != nil {
		return "", "", false, err
	}
	for _, connection := range connections {
		if !containsOrganization(connection.Organizations, organization) {
			continue
		}
		if !connection.Enabled {
			return "", connection.ID, true, fmt.Errorf("GitHub connection %q mapped to organization %q is disabled", connection.Name, organization)
		}
		token, err := c.ResolveAccessToken(ctx, user, connection.ID)
		return token, connection.ID, true, err
	}
	return "", "", false, nil
}
