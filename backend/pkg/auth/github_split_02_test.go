package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestGitHubAuthProvider_GetUserOrganizations(t *testing.T) {
	mockOrgs := []GitHubOrganization{
		{
			Login: "test-org",
			ID:    123,
		},
		{
			Login: "another-org",
			ID:    456,
		},
	}

	tests := []struct {
		name       string
		token      string
		mockStatus int
		mockOrgs   []GitHubOrganization
		wantErr    bool
	}{
		{
			name:       "successful request",
			token:      "valid-token",
			mockStatus: http.StatusOK,
			mockOrgs:   mockOrgs,
			wantErr:    false,
		},
		{
			name:       "unauthorized",
			token:      "invalid-token",
			mockStatus: http.StatusUnauthorized,
			mockOrgs:   nil,
			wantErr:    true,
		},
		{
			name:       "empty organizations",
			token:      "valid-token",
			mockStatus: http.StatusOK,
			mockOrgs:   []GitHubOrganization{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/user/orgs", r.URL.Path)
				assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatus)
				if tt.mockOrgs != nil {
					assert.NoError(t, json.NewEncoder(w).Encode(tt.mockOrgs))
				}
			}))
			defer mockServer.Close()

			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				BaseURL: mockServer.URL,
			})

			orgs, err := provider.getUserOrganizations(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.mockOrgs), len(orgs))
				for i, org := range orgs {
					assert.Equal(t, tt.mockOrgs[i].Login, org.Login)
					assert.Equal(t, tt.mockOrgs[i].ID, org.ID)
				}
			}
		})
	}
}

func TestGitHubAuthProvider_MapUserPermissions(t *testing.T) {
	tests := []struct {
		name               string
		teams              []GitHubTeamMembership
		config             config.GitHubUserMapping
		expectedRole       string
		expectedPermCount  int
		expectedPermExists []string
	}{
		{
			name: "user with admin team",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "admins",
					Role:         "maintainer",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/admins": {
						Role:        "admin",
						Permissions: []string{"admin", "write", "read"},
					},
				},
			},
			expectedRole:       "admin",
			expectedPermCount:  3,
			expectedPermExists: []string{"admin", "write", "read"},
		},
		{
			name:  "user with no team memberships",
			teams: []GitHubTeamMembership{},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
			},
			expectedRole:       "user",
			expectedPermCount:  1,
			expectedPermExists: []string{"read"},
		},
		{
			name: "user with multiple teams",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "developers",
					Role:         "member",
				},
				{
					Organization: "test-org",
					TeamSlug:     "reviewers",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/developers": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
					},
					"test-org/reviewers": {
						Role:        "member",
						Permissions: []string{"review"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3,
			expectedPermExists: []string{"read", "write", "review"},
		},
	}

	for _, tt := range tests {
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

func TestGitHubAuthProvider_MapUserPermissionsWithEnvFile(t *testing.T) {
	tests := []struct {
		name            string
		teams           []GitHubTeamMembership
		config          config.GitHubUserMapping
		expectedRole    string
		expectedEnvFile string
	}{
		{
			name: "team with env file",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "dev-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/dev-team": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
						EnvFile:     "/etc/agentapi/envs/dev-team.env",
					},
				},
			},
			expectedRole:    "developer",
			expectedEnvFile: "/etc/agentapi/envs/dev-team.env",
		},
		{
			name: "multiple teams with env files - higher role wins",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "dev-team",
					Role:         "member",
				},
				{
					Organization: "test-org",
					TeamSlug:     "admin-team",
					Role:         "maintainer",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/dev-team": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
						EnvFile:     "/etc/agentapi/envs/dev-team.env",
					},
					"test-org/admin-team": {
						Role:        "admin",
						Permissions: []string{"admin", "write", "read"},
						EnvFile:     "/etc/agentapi/envs/admin-team.env",
					},
				},
			},
			expectedRole:    "admin",
			expectedEnvFile: "/etc/agentapi/envs/admin-team.env",
		},
		{
			name: "team without env file",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "basic-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/basic-team": {
						Role:        "member",
						Permissions: []string{"read", "comment"},
					},
				},
			},
			expectedRole:    "member",
			expectedEnvFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: tt.config,
			})

			role, _, envFile := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedEnvFile, envFile)
		})
	}
}

func TestExtractTokenFromHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "Bearer token",
			header:   "Bearer gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "token prefix",
			header:   "token gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "raw token",
			header:   "gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "empty header",
			header:   "",
			expected: "",
		},
		{
			name:     "whitespace only",
			header:   "   ",
			expected: "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractTokenFromHeader(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGitHubAuthProvider_Integration(t *testing.T) {
	// Mock GitHub API server with full endpoints
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "token integration-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/user":
			user := GitHubUserInfo{
				Login: "integration-user",
				ID:    999999,
				Email: "integration@test.com",
				Name:  "Integration Test User",
			}
			assert.NoError(t, json.NewEncoder(w).Encode(user))

		case "/user/orgs":
			orgs := []GitHubOrganization{
				{Login: "test-org", ID: 12345},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(orgs))

		case "/user/teams":
			teams := []struct {
				ID           int64  `json:"id"`
				Name         string `json:"name"`
				Slug         string `json:"slug"`
				Permission   string `json:"permission"`
				Organization struct {
					Login string `json:"login"`
				} `json:"organization"`
			}{
				{
					ID:         1,
					Name:       "Developers",
					Slug:       "developers",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "test-org"},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(teams))

		case "/orgs/test-org/teams":
			teams := []struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			}{
				{Slug: "developers", Name: "Developers"},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(teams))

		case "/orgs/test-org/teams/developers/memberships/integration-user":
			membership := struct {
				State string `json:"state"`
				Role  string `json:"role"`
			}{
				State: "active",
				Role:  "member",
			}
			assert.NoError(t, json.NewEncoder(w).Encode(membership))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
		BaseURL: mockServer.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"test-org/developers": {
					Role:        "developer",
					Permissions: []string{"write", "read"},
				},
			},
		},
	})

	userCtx, err := provider.Authenticate(context.Background(), "integration-test-token")
	require.NoError(t, err)

	assert.Equal(t, "integration-user", userCtx.UserID)
	assert.Equal(t, "developer", userCtx.Role)
	assert.Contains(t, userCtx.Permissions, "read")
	assert.Contains(t, userCtx.Permissions, "write")
	assert.Equal(t, "github_oauth", userCtx.AuthType)
	assert.NotNil(t, userCtx.GitHubUser)
	assert.Equal(t, "integration@test.com", userCtx.GitHubUser.Email)
}
