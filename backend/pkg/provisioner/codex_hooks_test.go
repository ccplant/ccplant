package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestPrepareCodexHooksPreservesOrganisationPolicyAndUserHooks(t *testing.T) {
	home := t.TempDir()
	previous := codexRequirementsPath
	codexRequirementsPath = filepath.Join(home, "etc", "codex", "requirements.toml")
	t.Cleanup(func() { codexRequirementsPath = previous })
	require.NoError(t, writeCodexRequirementsToPath(codexRequirementsPath, `allowed_approval_policies = ["never"]
[hooks]
managed_dir = "/organisation/hooks"
[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = "notify"
timeout = 15
[[hooks.Stop.hooks]]
type = "command"
command = "cycle"
`))
	require.NoError(t, os.Chmod(codexRequirementsPath, 0444))
	path := filepath.Join(home, ".codex", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	user := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify","timeout":15},{"type":"command","command":"cycle"},{"type":"command","command":"user-only"}]},{"matcher":"other","hooks":[{"type":"command","command":"notify","timeout":15}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"user-prompt"}]}]},"custom":true}`
	require.NoError(t, os.WriteFile(path, []byte(user), 0644))
	injected := map[string]interface{}{"hooks": map[string]interface{}{"Stop": []map[string]interface{}{{"hooks": []map[string]interface{}{{"type": "command", "command": "cycle"}}}}}}
	require.NoError(t, prepareCodexHooks(home, injected))
	data, err := os.ReadFile(codexRequirementsPath)
	require.NoError(t, err)
	var managed map[string]interface{}
	require.NoError(t, toml.Unmarshal(data, &managed))
	require.Equal(t, []interface{}{"never"}, managed["allowed_approval_policies"])
	hooks := managed["hooks"].(map[string]interface{})
	require.Equal(t, "/organisation/hooks", hooks["managed_dir"])
	require.Len(t, hooks["Stop"], 1)
	stop := hooks["Stop"].([]interface{})[0].(map[string]interface{})
	notify := stop["hooks"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, int64(15), notify["timeout"])
	require.NotContains(t, string(data), "user-only")
	require.NotContains(t, string(data), "user-prompt")
	require.Contains(t, string(data), "timeout = 15")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	var remaining map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &remaining))
	require.JSONEq(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-only"}]},{"matcher":"other","hooks":[{"type":"command","command":"notify","timeout":15}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"user-prompt"}]}]},"custom":true}`, string(data))
	// Reprovisioning must neither append managed duplicates nor remove user hooks.
	require.NoError(t, prepareCodexHooks(home, injected))
	data, err = os.ReadFile(codexRequirementsPath)
	require.NoError(t, err)
	var repeated map[string]interface{}
	require.NoError(t, toml.Unmarshal(data, &repeated))
	require.Equal(t, managed, repeated)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	var repeatedUser map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &repeatedUser))
	require.Equal(t, remaining, repeatedUser)
}

func TestPrepareCodexHooksWritesOnlyInjectedHooksToNewRequirements(t *testing.T) {
	home := t.TempDir()
	previous := codexRequirementsPath
	codexRequirementsPath = filepath.Join(home, "etc", "codex", "requirements.toml")
	t.Cleanup(func() { codexRequirementsPath = previous })
	var injected map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(`{"hooks":{"Stop":[{"matcher":"fixture","hooks":[{"type":"command","command":"cycle","timeout":15,"async":true}]}],"Notification":[{"hooks":[{"type":"command","command":"claude-notify"}]}]}}`), &injected))
	path := filepath.Join(home, ".codex", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	user := `{"hooks":{"Stop":[{"matcher":"fixture","hooks":[{"type":"command","command":"cycle","timeout":15,"async":true},{"type":"command","command":"user-custom"}]}]}}`
	require.NoError(t, os.WriteFile(path, []byte(user), 0644))
	require.NoError(t, prepareCodexHooks(home, injected))
	data, err := os.ReadFile(codexRequirementsPath)
	require.NoError(t, err)
	var requirements map[string]interface{}
	require.NoError(t, toml.Unmarshal(data, &requirements))
	hooks := requirements["hooks"].(map[string]interface{})
	require.NotContains(t, hooks, "Notification")
	stop := hooks["Stop"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "fixture", stop["matcher"])
	command := stop["hooks"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, int64(15), command["timeout"])
	require.Equal(t, true, command["async"])
	require.NotContains(t, string(data), "user-custom")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, `{"hooks":{"Stop":[{"matcher":"fixture","hooks":[{"type":"command","command":"user-custom"}]}]}}`, string(data))
}

func TestPrepareCodexHooksKeepsUserFileWhenRequirementsCannotBeWritten(t *testing.T) {
	home := t.TempDir()
	previous := codexRequirementsPath
	blocker := filepath.Join(home, "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, nil, 0644))
	codexRequirementsPath = filepath.Join(blocker, "requirements.toml")
	t.Cleanup(func() { codexRequirementsPath = previous })
	path := filepath.Join(home, ".codex", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	user := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"cycle"}]}]}}`
	require.NoError(t, os.WriteFile(path, []byte(user), 0644))
	var injected map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(user), &injected))
	require.Error(t, prepareCodexHooks(home, injected))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, user, string(data))
}
