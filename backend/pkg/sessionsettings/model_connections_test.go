package sessionsettings

import (
	"encoding/json"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func TestManagedConnectionCredentialsAndPersistedEnvironment(t *testing.T) {
	for _, auth := range []string{"api_key", "bearer_token"} {
		s := &SessionSettings{Env: map[string]string{"ANTHROPIC_AUTH_TOKEN": "old", "ANTHROPIC_API_KEY": "old-key", "CLAUDE_CODE_USE_BEDROCK": "1", "CLAUDE_CODE_OAUTH_TOKEN": "oauth"}, ClaudeConnection: &modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://gateway.example", Model: "profile-model", Authentication: auth, APIKey: "gateway-key"}, Files: []ManagedFile{{Path: ManagedFileTypes[FileTypeClaudeCredentials], Content: "oauth"}, {Path: "/tmp/example", Content: "keep"}}}
		s.ApplyModelConnections()
		require.NotContains(t, s.Env, "CLAUDE_CODE_OAUTH_TOKEN")
		require.NotContains(t, s.Env, "CLAUDE_CODE_USE_BEDROCK")
		require.Len(t, s.Files, 1)
		require.Equal(t, "profile-model", s.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
		if auth == "api_key" {
			require.NotContains(t, s.Env, "ANTHROPIC_AUTH_TOKEN")
			require.Equal(t, "gateway-key", s.Env["ANTHROPIC_API_KEY"])
		} else {
			require.NotContains(t, s.Env, "ANTHROPIC_API_KEY")
			require.Equal(t, "gateway-key", s.Env["ANTHROPIC_AUTH_TOKEN"])
		}
		raw, err := json.Marshal(s)
		require.NoError(t, err)
		var restored SessionSettings
		require.NoError(t, json.Unmarshal(raw, &restored))
		require.Empty(t, restored.ClaudeConnection.APIKey)
		require.Equal(t, s.Env, restored.Env)
		require.Equal(t, s.UnsetEnv, restored.UnsetEnv)
	}
}
func TestCodexConfigReplacement(t *testing.T) {
	c := &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://gateway.example/v1", Model: "profile-model", Authentication: "api_key", APIKey: "never-in-toml"}
	base := "sandbox_mode = \"danger-full-access\"\nmodel_context_window = 128000\n[model_providers.agentapi_openai_compatible]\nbase_url = \"https://old.example\"\n"
	first, err := MergeCodexConnectionConfig(base, c)
	require.NoError(t, err)
	second, err := MergeCodexConnectionConfig(first, c)
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, toml.Unmarshal([]byte(second), &parsed))
	require.Equal(t, "profile-model", parsed["model"])
	require.Equal(t, "danger-full-access", parsed["sandbox_mode"])
	require.NotContains(t, parsed, "model_context_window")
	require.NotContains(t, second, "never-in-toml")
	require.Contains(t, second, "CCPLANT_CODEX_API_KEY")
	restored, err := MergeCodexConnectionConfig(second, &modelprovider.Connection{Mode: "auth_json"})
	require.NoError(t, err)
	require.NotContains(t, restored, "gateway.example")
}
