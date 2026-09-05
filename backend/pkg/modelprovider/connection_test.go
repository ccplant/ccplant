package modelprovider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelLayerPrecedence(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		key := "CODEX_MODEL"
		if agent == "claude" {
			key = "ANTHROPIC_MODEL"
		}
		require.Equal(t, "default", ModelForLayers(agent, "default", nil, map[string]string{key: " "}))
		require.Equal(t, "profile", ModelForLayers(agent, "default", map[string]string{key: "profile"}, nil))
		require.Equal(t, "request", ModelForLayers(agent, "default", map[string]string{key: "profile"}, map[string]string{key: "request"}))
	}
	require.Equal(t, "request-legacy", ModelForLayers("codex", "default", map[string]string{"CODEX_MODEL": "profile"}, map[string]string{"OPENAI_MODEL": "request-legacy"}))
	require.Equal(t, "codex", ModelForLayers("codex", "default", map[string]string{"CODEX_MODEL": "codex", "OPENAI_MODEL": "legacy"}))
}
func TestConnectionValidation(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		mode := "openai_compatible"
		if agent == "claude" {
			mode = "anthropic_compatible"
		}
		c := &Connection{Mode: mode, BaseURL: "https://gateway.example/api", Model: "model", Authentication: "api_key", APIKey: "secret"}
		require.NoError(t, c.Validate(agent))
		for _, bad := range []string{"https://user:secret@example.com", "file:///tmp/model", "https://example.com/api?token=secret", "https://example.com/v1/messages", "https://example.com/v1/responses"} {
			copy := c.Clone()
			copy.BaseURL = bad
			require.Error(t, copy.Validate(agent))
		}
		copy := c.Clone()
		copy.Authentication = "none"
		copy.APIKey = ""
		if agent == "codex" {
			require.NoError(t, copy.Validate(agent))
		} else {
			require.Error(t, copy.Validate(agent))
		}
	}
}
