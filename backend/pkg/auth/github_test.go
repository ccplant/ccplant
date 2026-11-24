package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewGitHubAuthProvider(t *testing.T) {
	tests := []struct {
		name   string
		config *config.GitHubAuthConfig
	}{
		{
			name: "valid config",
			config: &config.GitHubAuthConfig{
				Enabled: true,
				BaseURL: "https://api.github.com",
			},
		},
		{
			name:   "nil config",
			config: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(tt.config)
			assert.NotNil(t, provider)
		})
	}
}

func TestGitHubAuthProvider_Authenticate(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		mockResponse *GitHubUserInfo
		mockStatus   int
		wantUser     *UserContext
		wantErr      bool
	}{
		{
			name:  "successful authentication",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Email: "test@example.com",
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantUser: &UserContext{
				UserID:   "testuser",
				AuthType: "github_oauth",
			},
			wantErr: false,
		},
		{
			name:         "unauthorized token",
			token:        "invalid-token",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantUser:     nil,
			wantErr:      true,
		},
		{
			name:         "empty token",
			token:        "",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantUser:     nil,
			wantErr:      true,
		},
		{
			name:  "user without email",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantUser: &UserContext{
				UserID:   "testuser",
				AuthType: "github_oauth",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					assert.Equal(t, "GET", r.Method)
					// Only check authorization header if token is not empty
					if tt.token != "" {
						assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))
					}
					w.WriteHeader(tt.mockStatus)
					if tt.mockResponse != nil {
						assert.NoError(t, json.NewEncoder(w).Encode(tt.mockResponse))
					}
				case "/user/orgs":
					w.WriteHeader(http.StatusOK)
					assert.NoError(t, json.NewEncoder(w).Encode([]GitHubOrganization{}))
				case "/orgs/test-org/teams/developers/memberships/testuser":
					w.WriteHeader(http.StatusOK)
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
						"state": "active",
						"role":  "member",
					}))
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
							Role:        "user",
							Permissions: []string{"read"},
						},
					},
				},
			})

			user, err := provider.Authenticate(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantUser.UserID, user.UserID)
				assert.Equal(t, tt.wantUser.AuthType, user.AuthType)
			}
		})
	}
}

func TestGitHubAuthProviderCachedTeamMemberships(t *testing.T) {
	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{})
	provider.teamCache.Set("alice", []GitHubTeamMembership{{Organization: "ccplant", TeamSlug: "developers"}})

	teams, found, err := provider.CachedTeamMemberships(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []GitHubTeamMembership{{Organization: "ccplant", TeamSlug: "developers"}}, teams)

	teams[0].TeamSlug = "changed"
	again, found, err := provider.CachedTeamMemberships(context.Background(), "alice")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "developers", again[0].TeamSlug)
}

func TestGitHubAuthProvider_AuthenticateCoalescesConcurrentRequests(t *testing.T) {
	var userRequests atomic.Int32
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			userRequests.Add(1)
			time.Sleep(50 * time.Millisecond)
			assert.NoError(t, json.NewEncoder(w).Encode(&GitHubUserInfo{Login: "testuser", ID: 12345}))
		case "/orgs/test-org/teams/developers/memberships/testuser":
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"state": "active", "role": "member"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
		BaseURL: mockServer.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole: "user",
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"test-org/developers": {Role: "user"},
			},
		},
	})

	const callers = 8
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := provider.Authenticate(context.Background(), "same-token")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), userRequests.Load())
}

func TestGitHubAuthProvider_AuthenticateRejectsUserOutsideConfiguredTeams(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.WriteHeader(http.StatusOK)
			assert.NoError(t, json.NewEncoder(w).Encode(&GitHubUserInfo{
				Login: "outsider",
				ID:    12345,
			}))
		case "/orgs/test-org/teams/developers/memberships/outsider":
			w.WriteHeader(http.StatusNotFound)
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
					Role:        "user",
					Permissions: []string{"read"},
				},
			},
		},
	})

	user, err := provider.Authenticate(context.Background(), "valid-github-token")

	require.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "does not belong to any configured team")
}

func TestGitHubAuthProvider_AuthenticateAllowsUserOutsideConfiguredTeamsWhenEnabled(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.WriteHeader(http.StatusOK)
			assert.NoError(t, json.NewEncoder(w).Encode(&GitHubUserInfo{
				Login: "outsider",
				ID:    12345,
			}))
		case "/orgs/test-org/teams/developers/memberships/outsider":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
		BaseURL: mockServer.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:           "guest",
			DefaultPermissions:    []string{"read"},
			AllowUsersWithoutTeam: true,
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"test-org/developers": {
					Role:        "developer",
					Permissions: []string{"write"},
				},
			},
		},
	})

	user, err := provider.Authenticate(context.Background(), "valid-github-token")

	require.NoError(t, err)
	assert.Equal(t, "outsider", user.UserID)
	assert.Equal(t, "guest", user.Role)
	assert.Equal(t, []string{"read"}, user.Permissions)
	assert.Empty(t, user.GitHubUser.Teams)
}

func TestGitHubAuthProvider_GetUser(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		mockResponse *GitHubUserInfo
		mockStatus   int
		wantErr      bool
	}{
		{
			name:  "successful request",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Email: "test@example.com",
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:         "unauthorized",
			token:        "invalid-token",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantErr:      true,
		},
		{
			name:         "server error",
			token:        "valid-token",
			mockResponse: nil,
			mockStatus:   http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/user", r.URL.Path)
				assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatus)
				if tt.mockResponse != nil {
					assert.NoError(t, json.NewEncoder(w).Encode(tt.mockResponse))
				}
			}))
			defer mockServer.Close()

			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				BaseURL: mockServer.URL,
			})

			user, err := provider.getUser(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mockResponse.Login, user.Login)
				assert.Equal(t, tt.mockResponse.ID, user.ID)
				assert.Equal(t, tt.mockResponse.Email, user.Email)
			}
		})
	}
}
