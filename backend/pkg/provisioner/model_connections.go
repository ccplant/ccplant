package provisioner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func withoutEnvironment(env []string, unset []string) []string {
	blocked := map[string]bool{}
	for _, k := range unset {
		blocked[k] = true
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		k, _, _ := strings.Cut(entry, "=")
		if !blocked[k] {
			out = append(out, entry)
		}
	}
	return out
}

// Clean restored user config after all managed files have been written, before launch.
func cleanModelConnectionFiles(settings *sessionsettings.SessionSettings, home string) error {
	for _, path := range settings.RemoveFiles {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove inactive credential file: %w", err)
		}
	}
	if c := settings.CodexConnection; c != nil {
		path := filepath.Join(home, ".codex", "config.toml")
		raw, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		content, err := sessionsettings.MergeCodexConnectionConfig(string(raw), c)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return err
		}
	}
	if settings.ClaudeConnection == nil {
		return nil
	}
	path := filepath.Join(home, ".claude", "settings.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var config map[string]interface{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("invalid restored Claude settings")
	}
	delete(config, "apiKeyHelper")
	delete(config, "model")
	if env, ok := config["env"].(map[string]interface{}); ok {
		keys := modelprovider.ConnectionEnvKeys("claude")
		if !settings.ClaudeConnection.Compatible() {
			keys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"}
		}
		keys = append(keys, "CLAUDE_CODE_USE_BEDROCK")
		for _, key := range keys {
			delete(env, key)
		}
		for key := range settings.Env {
			if strings.HasPrefix(key, "ANTHROPIC_") {
				delete(env, key)
			}
		}
	}
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

// The archive carries only routing metadata, never the connection secret. A
// resume must not silently send the restored conversation to a different API.
func persistModelConnectionIdentity(settings *sessionsettings.SessionSettings, home string, restoring bool) error {
	type identity struct {
		Agent          string `json:"agent"`
		Mode           string `json:"mode"`
		BaseURL        string `json:"base_url"`
		Model          string `json:"model"`
		Authentication string `json:"authentication"`
	}
	var current *identity
	c, agent := settings.CodexConnection, "codex"
	if c == nil {
		c, agent = settings.ClaudeConnection, "claude"
	}
	if c != nil {
		current = &identity{Agent: agent, Mode: c.Mode}
		if c.Compatible() {
			current.BaseURL = c.BaseURL
			current.Model = c.Model
			current.Authentication = c.Authentication
		}
	}
	path := filepath.Join(home, ".session", "model-connection.json")
	if restoring {
		raw, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if len(raw) > 0 {
			var previous identity
			if err := json.Unmarshal(raw, &previous); err != nil {
				return fmt.Errorf("invalid restored connection identity")
			}
			if current == nil || previous != *current {
				return fmt.Errorf("restored session connection or model differs from the selected profile")
			}
		} else if current != nil {
			return fmt.Errorf("restored session has no connection identity; start a new session")
		}
	}
	if current == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	raw, err := json.Marshal(current)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}
