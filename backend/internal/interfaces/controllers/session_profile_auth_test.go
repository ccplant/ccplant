package controllers

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"testing"
)

func TestProfileAuthMethodsValidationAndMerge(t *testing.T) {
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{CodexAuthMode: "oauth"})
	require.Error(t, validateSessionProfileConfig(cfg))
	cfg.SetParams(&entities.SessionParams{CodexAuthMode: "auth_json", ClaudeAuthMode: "anthropic_compatible"})
	require.NoError(t, validateSessionProfileConfig(cfg))
	cfg.SetParams(&entities.SessionParams{Model: "invalid\nmodel"})
	require.Error(t, validateSessionProfileConfig(cfg))
	cfg.SetParams(&entities.SessionParams{Model: "gpt-test"})
	require.NoError(t, validateSessionProfileConfig(cfg))
	cfg.SetParams(&entities.SessionParams{CodexAuthMode: "auth_json", ClaudeAuthMode: "anthropic_compatible", Model: "gpt-test"})
	merged := mergeSessionParams(cfg.Params(), &entities.SessionParams{ClaudeAuthMode: "oauth"})
	require.Equal(t, "auth_json", merged.CodexAuthMode)
	require.Equal(t, "oauth", merged.ClaudeAuthMode)
	require.Equal(t, "anthropic_compatible", cfg.Params().ClaudeAuthMode)
}

func TestMergeSessionParamsRequestPoolOverridesProfilePool(t *testing.T) {
	profile := &entities.SessionParams{Pool: "profile-pool"}
	merged := mergeSessionParams(profile, &entities.SessionParams{Pool: "request-pool"})

	require.Equal(t, "request-pool", merged.Pool)
	require.Equal(t, "profile-pool", profile.Pool)
}

func TestProfileConnectionSecretLifecycle(t *testing.T) {
	raw := []byte(`{"mode":"openai_compatible","base_url":"https://profile.example/v1","authentication":"api_key","api_key":"profile-secret"}`)
	connection, err := mergeProfileConnection(nil, raw, "codex")
	require.NoError(t, err)
	require.Empty(t, connection.Model)
	require.Equal(t, "profile-secret", connection.APIKey)
	updated, err := mergeProfileConnection(connection, []byte(`{"mode":"openai_compatible","base_url":"https://updated.example/v1","authentication":"api_key","model":"profile-model"}`), "codex")
	require.NoError(t, err)
	require.Equal(t, "profile-secret", updated.APIKey)
	require.Equal(t, "https://profile.example/v1", connection.BaseURL)
	kept, err := mergeProfileConnection(updated, nil, "codex")
	require.NoError(t, err)
	require.Equal(t, updated, kept)
	cleared, err := mergeProfileConnection(updated, []byte(`null`), "codex")
	require.NoError(t, err)
	require.Nil(t, cleared)
	_, err = mergeProfileConnection(nil, []byte(`{"mode":"openai_compatible","base_url":"https://profile.example/v1","authentication":"api_key"}`), "codex")
	require.Error(t, err)
	_, err = mergeProfileConnection(nil, []byte(`{"mode":"oauth"}`), "claude")
	require.Error(t, err)

	cfg := entities.NewSessionProfileConfig()
	cfg.SetCodexConnection(connection)
	profile := entities.NewSessionProfile("profile", "Profile", "user")
	profile.SetConfig(cfg)
	response := (&SessionProfileController{}).toResponse(profile)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "profile-secret")
	require.Contains(t, string(encoded), `"has_api_key":true`)
	require.NotContains(t, string(encoded), `"api_key":`)
}

func TestProfileConnectionEndpointAndWebSearch(t *testing.T) {
	c, err := mergeProfileConnection(nil, []byte(`{"mode":"openai_compatible","base_url":"https://gateway.example","endpoint_path":"/api/generate","authentication":"none","web_search_enabled":false}`), "codex")
	require.NoError(t, err)
	require.Equal(t, "/api/generate", c.EndpointPath)
	require.NotNil(t, c.WebSearchEnabled)
	require.False(t, *c.WebSearchEnabled)
	kept, err := mergeProfileConnection(c, []byte(`{"mode":"openai_compatible"}`), "codex")
	require.NoError(t, err)
	require.Equal(t, c, kept)
	cleared, err := mergeProfileConnection(c, []byte(`{"mode":"openai_compatible","endpoint_path":"","web_search_enabled":null}`), "codex")
	require.NoError(t, err)
	require.Empty(t, cleared.EndpointPath)
	require.Nil(t, cleared.WebSearchEnabled)
	require.False(t, *c.WebSearchEnabled)
	for _, path := range []string{"relative", "//other.example/path", "/../secret", "/api?key=secret", "/api#fragment", "/api/%2e%2e/path", "/api//path"} {
		raw, err := json.Marshal(map[string]interface{}{"mode": "openai_compatible", "endpoint_path": path})
		require.NoError(t, err)
		_, err = mergeProfileConnection(c, raw, "codex")
		require.Error(t, err, path)
	}
}

func TestProfileTeamSettingsAccess(t *testing.T) {
	user := entities.NewUser("user", entities.UserTypeGitHub, "user")
	user.SetGitHubInfo(entities.NewGitHubUserInfo(1, "user", "", "", "", "", ""), []entities.GitHubTeamMembership{{Organization: "org", TeamSlug: "team"}})
	p := entities.NewSessionProfile("profile", "Profile", "user")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetSettingsTeamID("org/team")
	require.NoError(t, validateProfileSettingsTeam(user, p, cfg))
	cfg.SetSettingsTeamID("org/other")
	require.Error(t, validateProfileSettingsTeam(user, p, cfg))
	p.SetScope(entities.ScopeTeam)
	p.SetTeamID("org/other")
	cfg.SetSettingsTeamID("org/team")
	require.Error(t, validateProfileSettingsTeam(user, p, cfg))
}
