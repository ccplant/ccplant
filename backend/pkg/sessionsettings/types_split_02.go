package sessionsettings

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ClaudeConfig struct {
	ClaudeJSON   map[string]interface{} `yaml:"claude_json,omitempty"    json:"claude_json,omitempty"`
	SettingsJSON map[string]interface{} `yaml:"settings_json,omitempty"  json:"settings_json,omitempty"`
	MCPServers   map[string]interface{} `yaml:"mcp_servers,omitempty"    json:"mcp_servers,omitempty"`
}

// CodexConfig holds Codex CLI-specific configuration data.
type CodexConfig struct {
	// HooksJSON is written to ~/.codex/hooks.json (codex-acp sessions only).
	HooksJSON map[string]interface{} `yaml:"hooks_json,omitempty"  json:"hooks_json,omitempty"`
	// ConfigTOML is written to ~/.codex/config.toml (codex-acp sessions only).
	// Use it to set approval-mode and other Codex CLI settings that bypass permission prompts.
	ConfigTOML string `yaml:"config_toml,omitempty" json:"config_toml,omitempty"`
	// InstructionsMD is written to ~/.codex/AGENTS.md.
	// It provides user-level instructions to the Codex CLI, equivalent to
	// ~/.claude/CLAUDE.md for Claude Code.  When non-empty it overrides the
	// default instructions baked into the Docker image.
	InstructionsMD string `yaml:"instructions_md,omitempty" json:"instructions_md,omitempty"`
	// MCPServers holds MCP server configurations keyed by server name.
	// Each entry is appended to ~/.codex/config.toml as a [[mcp_servers]] TOML
	// array-of-tables entry so the Codex CLI can read them natively.
	// The map format mirrors ClaudeConfig.MCPServers for consistency.
	MCPServers map[string]interface{} `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
}

// PiConfig holds Pi coding agent-specific configuration data.
type PiConfig struct {
	// SettingsJSON is merged into ~/.pi/agent/settings.json.
	SettingsJSON map[string]interface{} `yaml:"settings_json,omitempty" json:"settings_json,omitempty"`
	// ModelsJSON is merged into ~/.pi/agent/models.json.
	ModelsJSON map[string]interface{} `yaml:"models_json,omitempty" json:"models_json,omitempty"`
}

// RepositoryConfig holds repository information.
type RepositoryConfig struct {
	FullName string `yaml:"fullname"   json:"fullname"`
	CloneDir string `yaml:"clone_dir"  json:"clone_dir"`
	Branch   string `yaml:"branch,omitempty" json:"branch,omitempty"`
	PR       string `yaml:"pr,omitempty"     json:"pr,omitempty"`
}

// StartupConfig holds the startup command configuration.
type StartupConfig struct {
	Command                []string `yaml:"command,omitempty"                  json:"command,omitempty"`
	Args                   []string `yaml:"args,omitempty"                     json:"args,omitempty"`
	PreScript              string   `yaml:"pre_script,omitempty"               json:"pre_script,omitempty"`
	CommandWrapperTemplate string   `yaml:"command_wrapper_template,omitempty" json:"command_wrapper_template,omitempty"`
}

// GithubConfig holds GitHub authentication configuration reference info.
type GithubConfig struct {
	Token            string `yaml:"token,omitempty"              json:"token,omitempty"`
	SecretName       string `yaml:"secret_name,omitempty"        json:"secret_name,omitempty"`
	ConfigSecretName string `yaml:"config_secret_name,omitempty" json:"config_secret_name,omitempty"`
}

// LoadSettings reads and parses a SessionSettings from a YAML file.
func LoadSettings(path string) (*SessionSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings SessionSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings YAML: %w", err)
	}

	return &settings, nil
}

// LoadSettingsFromBytes parses a SessionSettings from YAML bytes.
func LoadSettingsFromBytes(data []byte) (*SessionSettings, error) {
	var settings SessionSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings YAML: %w", err)
	}
	return &settings, nil
}

// MarshalYAML marshals SessionSettings to YAML bytes.
func MarshalYAML(settings *SessionSettings) ([]byte, error) {
	return yaml.Marshal(settings)
}
