package provisioner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestModelEnvironmentIsolation(t *testing.T) {
	env := mergeEnv(withoutEnvironment([]string{"ANTHROPIC_AUTH_TOKEN=old", "ANTHROPIC_API_KEY=old-key", "PATH=/bin"}, []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"}), map[string]string{"ANTHROPIC_API_KEY": "selected"})
	require.Contains(t, env, "PATH=/bin")
	require.Contains(t, env, "ANTHROPIC_API_KEY=selected")
	require.NotContains(t, env, "ANTHROPIC_AUTH_TOKEN=old")
}
func TestRestoredClaudeCredentialsRemoved(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(dir, 0700))
	credentials := filepath.Join(dir, ".credentials.json")
	require.NoError(t, os.WriteFile(credentials, []byte("old-oauth"), 0600))
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"apiKeyHelper":"old-helper","env":{"ANTHROPIC_AUTH_TOKEN":"old-token","KEEP":"yes"},"hooks":{}}`), 0600))
	settings := &sessionsettings.SessionSettings{ClaudeConnection: &modelprovider.Connection{Mode: "anthropic_compatible"}, RemoveFiles: []string{credentials}}
	require.NoError(t, cleanModelConnectionFiles(settings, home))
	_, err := os.Stat(credentials)
	require.True(t, os.IsNotExist(err))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "old-token")
	require.NotContains(t, string(raw), "apiKeyHelper")
	require.Contains(t, string(raw), "KEEP")
	require.Contains(t, string(raw), "hooks")
}

func TestResumeRejectsChangedConnection(t *testing.T) {
	home := t.TempDir()
	settings := &sessionsettings.SessionSettings{CodexConnection: &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://original.example/v1", Model: "profile", Authentication: "api_key", APIKey: "private"}}
	require.NoError(t, persistModelConnectionIdentity(settings, home, false))
	raw, err := os.ReadFile(filepath.Join(home, ".session", "model-connection.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	require.NoError(t, persistModelConnectionIdentity(settings, home, true))
	settings.CodexConnection.BaseURL = "https://other.example/v1"
	require.Error(t, persistModelConnectionIdentity(settings, home, true))
	require.Error(t, persistModelConnectionIdentity(&sessionsettings.SessionSettings{}, home, true))
}
