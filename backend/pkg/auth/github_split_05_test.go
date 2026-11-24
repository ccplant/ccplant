package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

var wildcardPermissionTests = []struct {
	name               string
	teams              []GitHubTeamMembership
	config             config.GitHubUserMapping
	expectedRole       string
	expectedPermCount  int
	expectedPermExists []string
}{
	{
		name: "wildcard pattern matches team",
		teams: []GitHubTeamMembership{
			{
				Organization: "org-alpha",
				TeamSlug:     "cc-users",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"write", "deploy"},
				},
			},
		},
		expectedRole:       "developer",
		expectedPermCount:  3, // read + write + deploy
		expectedPermExists: []string{"read", "write", "deploy"},
	},
	{
		name: "wildcard pattern matches multiple orgs",
		teams: []GitHubTeamMembership{
			{
				Organization: "org-alpha",
				TeamSlug:     "cc-users",
				Role:         "member",
			},
			{
				Organization: "org-beta",
				TeamSlug:     "cc-users",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
			},
		},
		expectedRole:       "developer",
		expectedPermCount:  2, // read + write
		expectedPermExists: []string{"read", "write"},
	},
	{
		name: "exact match and wildcard both apply - permissions combined",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "cc-users",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
				"myorg/cc-users": {
					Role:        "admin",
					Permissions: []string{"admin"},
				},
			},
		},
		expectedRole:       "admin",
		expectedPermCount:  3, // read + write + admin (both rules apply)
		expectedPermExists: []string{"read", "write", "admin"},
	},
	{
		name: "highest role wins across multiple wildcard matches",
		teams: []GitHubTeamMembership{
			{
				Organization: "org-a",
				TeamSlug:     "developers",
				Role:         "member",
			},
			{
				Organization: "org-b",
				TeamSlug:     "admins",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "guest",
			DefaultPermissions: []string{},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/developers": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
				"*/admins": {
					Role:        "admin",
					Permissions: []string{"admin"},
				},
			},
		},
		expectedRole:       "admin",
		expectedPermCount:  2,
		expectedPermExists: []string{"write", "admin"},
	},
	{
		name: "team suffix pattern matches",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "backend-engineer",
				Role:         "member",
			},
			{
				Organization: "myorg",
				TeamSlug:     "frontend-engineer",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/*-engineer": {
					Role:        "developer",
					Permissions: []string{"write", "execute"},
				},
			},
		},
		expectedRole:       "developer",
		expectedPermCount:  3, // read + write + execute
		expectedPermExists: []string{"read", "write", "execute"},
	},
	{
		name: "team prefix pattern matches",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "backend-team",
				Role:         "member",
			},
			{
				Organization: "myorg",
				TeamSlug:     "backend-developers",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/backend-*": {
					Role:        "developer",
					Permissions: []string{"write", "debug"},
				},
			},
		},
		expectedRole:       "developer",
		expectedPermCount:  3, // read + write + debug
		expectedPermExists: []string{"read", "write", "debug"},
	},
	{
		name: "team wildcard pattern matches all teams",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "team-a",
				Role:         "member",
			},
			{
				Organization: "myorg",
				TeamSlug:     "team-b",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/*": {
					Role:        "member",
					Permissions: []string{"write"},
				},
			},
		},
		expectedRole:       "member",
		expectedPermCount:  2, // read + write
		expectedPermExists: []string{"read", "write"},
	},
	{
		name: "suffix pattern no match",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "backend-team",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/*-engineer": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
			},
		},
		expectedRole:       "user",
		expectedPermCount:  1,
		expectedPermExists: []string{"read"},
	},
	{
		name: "prefix pattern no match",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "frontend-team",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/backend-*": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
			},
		},
		expectedRole:       "user",
		expectedPermCount:  1,
		expectedPermExists: []string{"read"},
	},
	{
		name: "combined patterns - multiple matches",
		teams: []GitHubTeamMembership{
			{
				Organization: "myorg",
				TeamSlug:     "backend-engineer",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"myorg/backend-*": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
				"myorg/*-engineer": {
					Role:        "developer",
					Permissions: []string{"execute"},
				},
			},
		},
		expectedRole:       "developer",
		expectedPermCount:  3, // read + write + execute (both patterns match)
		expectedPermExists: []string{"read", "write", "execute"},
	},
	{
		name: "no match - default only",
		teams: []GitHubTeamMembership{
			{
				Organization: "org-a",
				TeamSlug:     "other-team",
				Role:         "member",
			},
		},
		config: config.GitHubUserMapping{
			DefaultRole:        "guest",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
			},
		},
		expectedRole:       "guest",
		expectedPermCount:  1,
		expectedPermExists: []string{"read"},
	},
}

func TestGitHubAuthProvider_MapUserPermissionsWithWildcard(t *testing.T) {

	for _, tt := range wildcardPermissionTests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: tt.config,
			})

			role, permissions, _ := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedPermCount, len(permissions))

			for _, expectedPerm := range tt.expectedPermExists {
				assert.Contains(t, permissions, expectedPerm)
			}
		})
	}
}
