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

func TestGitHubAuthProvider_WildcardIntegration(t *testing.T) {
	// Mock GitHub API server with /user/teams endpoint
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "token wildcard-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/user":
			user := GitHubUserInfo{
				Login: "wildcard-user",
				ID:    123456,
			}
			assert.NoError(t, json.NewEncoder(w).Encode(user))

		case "/user/teams":
			teams := []struct {
				ID           int64  `json:"id"`
				Slug         string `json:"slug"`
				Name         string `json:"name"`
				Permission   string `json:"permission"`
				Organization struct {
					Login string `json:"login"`
				} `json:"organization"`
			}{
				{
					ID:         1,
					Slug:       "cc-users",
					Name:       "CC Users",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-alpha"},
				},
				{
					ID:         2,
					Slug:       "cc-users",
					Name:       "CC Users",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-beta"},
				},
				{
					ID:         3,
					Slug:       "other-team",
					Name:       "Other Team",
					Permission: "pull",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-alpha"},
				},
			}
			assert.NoError(t, json.NewEncoder(w).Encode(teams))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
		BaseURL: mockServer.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "guest",
			DefaultPermissions: []string{},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"read", "write"},
				},
			},
		},
	})

	userCtx, err := provider.Authenticate(context.Background(), "wildcard-test-token")
	require.NoError(t, err)

	assert.Equal(t, "wildcard-user", userCtx.UserID)
	assert.Equal(t, "developer", userCtx.Role)
	assert.Contains(t, userCtx.Permissions, "read")
	assert.Contains(t, userCtx.Permissions, "write")

	// Verify that both org-alpha/cc-users and org-beta/cc-users matched
	assert.Len(t, userCtx.GitHubUser.Teams, 2)
}
