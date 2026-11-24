package sessionsettings

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// CompileOptions configures the compile-settings behavior.
type CompileOptions struct {
	InputPath string // Path to settings YAML (default: /session-settings/settings.yaml)
	OutputDir string // Base output directory (default: /home/agentapi)
	// EnvFilePath is retained for CLI compatibility. Environment variables are
	// no longer written to disk.
	EnvFilePath string
	StartupPath string // Path for startup script (default: /session-settings/startup.sh)
}

// DefaultCompileOptions returns the default compile options.
func DefaultCompileOptions() CompileOptions {
	homeDir := "/home/agentapi"
	if strings.TrimSpace(os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT")) != "" {
		if nativeHome, err := os.UserHomeDir(); err == nil && nativeHome != "" {
			homeDir = nativeHome
		}
	}
	return CompileOptions{
		InputPath:   "/session-settings/settings.yaml",
		OutputDir:   homeDir,
		EnvFilePath: filepath.Join(homeDir, ".session", "env"),
		StartupPath: filepath.Join(homeDir, ".session", "startup.sh"),
	}
}

// Compile reads the settings YAML and generates all output files.
func Compile(opts CompileOptions) error {
	log.Printf("[COMPILE-SETTINGS] Reading settings from %s", opts.InputPath)

	// 1. Read and parse YAML
	data, err := os.ReadFile(opts.InputPath)
	if err != nil {
		return fmt.Errorf("failed to read settings file %s: %w", opts.InputPath, err)
	}

	var settings SessionSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings YAML: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Loaded settings for session %s (user: %s)", settings.Session.ID, settings.Session.UserID)
	return CompileSettings(&settings, opts)
}

// CompileSettings generates agent configuration files from settings held in
// memory. Environment variables may influence generated configuration, but are
// never serialized to a standalone file.
func CompileSettings(settings *SessionSettings, opts CompileOptions) error {
	// 2. Generate ~/.claude.json (includes mcpServers if present)
	if err := generateClaudeJSON(opts.OutputDir, settings.Claude.ClaudeJSON, settings.Claude.MCPServers); err != nil {
		return fmt.Errorf("failed to generate .claude.json: %w", err)
	}

	// 3. Generate ~/.claude/settings.json
	if err := generateSettingsJSON(opts.OutputDir, settings.Claude.SettingsJSON); err != nil {
		return fmt.Errorf("failed to generate settings.json: %w", err)
	}

	// 3b. Generate ~/.codex/hooks.json (codex-acp sessions only)
	if err := generateCodexHooksJSON(opts.OutputDir, settings.Codex.HooksJSON); err != nil {
		return fmt.Errorf("failed to generate codex hooks.json: %w", err)
	}

	// 3c. Generate ~/.codex/config.toml (codex-acp sessions only)
	codexConfig, codexEnv := settings.Codex.ConfigTOML, settings.Env
	// Model candidates are registered through a Codex catalog, so the catalog
	// env survives even when an explicit connection owns the provider TOML.
	catalogEnv := codexModelCatalogEnv(settings)
	if settings.CodexConnection != nil {
		codexEnv = nil
		if settings.CodexConnection.Compatible() {
			var err error
			codexConfig, err = MergeCodexConnectionConfig(codexConfig, settings.CodexConnection)
			if err != nil {
				return err
			}
		}
	}
	if err := generateCodexConfigTOML(opts.OutputDir, codexConfig, codexEnv, catalogEnv); err != nil {
		return fmt.Errorf("failed to generate codex config.toml: %w", err)
	}
	// codex-acp starts Codex app-server before it creates the ACP session. Pass
	// the selected provider through its supported environment interface as well
	// as writing config.toml, so authentication is evaluated against the managed
	// provider even when a Codex release does not load the on-disk user config.
	if settings.CodexConnection != nil && settings.CodexConnection.Compatible() {
		var config map[string]interface{}
		if err := toml.Unmarshal([]byte(codexConfig), &config); err != nil {
			return fmt.Errorf("failed to build codex-acp config: %w", err)
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to encode codex-acp config: %w", err)
		}
		settings.Env["CODEX_CONFIG"] = string(encoded)
		settings.Env["MODEL_PROVIDER"] = codexCustomOpenAIProviderID
	}

	// 3d. Generate ~/.codex/AGENTS.md (codex sessions only)
	if err := generateCodexInstructionsMD(opts.OutputDir, settings.Codex.InstructionsMD); err != nil {
		return fmt.Errorf("failed to generate codex AGENTS.md: %w", err)
	}

	// 3e. Append MCP server entries to ~/.codex/config.toml (codex sessions only)
	if err := generateCodexMCPServers(opts.OutputDir, settings.Env, settings.Codex.MCPServers); err != nil {
		return fmt.Errorf("failed to generate codex MCP servers config: %w", err)
	}

	// 3f. Generate the shared MCP config read by pi-mcp-adapter (pi sessions only)
	if err := generatePiMCPConfig(opts.OutputDir, settings.Session.AgentType, settings.Claude.MCPServers); err != nil {
		return fmt.Errorf("failed to generate Pi MCP config: %w", err)
	}

	// 3g. Generate ~/.pi/agent/settings.json when managed Pi settings are present.
	if err := generatePiSettingsJSON(opts.OutputDir, settings.Pi.SettingsJSON); err != nil {
		return fmt.Errorf("failed to generate Pi settings.json: %w", err)
	}

	// 3h. Generate ~/.pi/agent/models.json when a custom model is configured.
	if err := generatePiModelsJSON(opts.OutputDir, settings.Pi.ModelsJSON); err != nil {
		return fmt.Errorf("failed to generate Pi models.json: %w", err)
	}

	// 4. Generate startup script
	if err := generateStartupScript(opts.StartupPath, settings.Startup); err != nil {
		return fmt.Errorf("failed to generate startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Successfully compiled all configuration files")
	return nil
}

// generateClaudeJSON creates ~/.claude.json with onboarding settings and MCP server configuration.
// Mirrors the pattern from pkg/startup/sync.go generateClaudeJSON (lines 157-188).
// mcpServers, if non-empty, is written to the "mcpServers" key so Claude Code can read it natively.
func generateClaudeJSON(outputDir string, claudeJSON map[string]interface{}, mcpServers map[string]interface{}) error {
	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	claudeJSONPath := filepath.Join(outputDir, ".claude.json")

	// Read existing file if present
	var existing map[string]interface{}
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			log.Printf("[COMPILE-SETTINGS] Warning: failed to parse existing .claude.json: %v", err)
			existing = make(map[string]interface{})
		}
	} else {
		existing = make(map[string]interface{})
	}

	// Merge in provided values
	for k, v := range claudeJSON {
		existing[k] = v
	}

	// Always set required onboarding fields
	existing["hasCompletedOnboarding"] = true
	existing["bypassPermissionsModeAccepted"] = true

	// Write MCP servers directly into claude.json so Claude Code reads them natively
	if len(mcpServers) > 0 {
		existing["mcpServers"] = mcpServers
	}

	// Write file
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}

	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", claudeJSONPath)
	return nil
}

// patchClaudeJSON reads the existing ~/.claude.json (written by Claude CLI or
// compile step) and ensures the required onboarding fields are set to true.
// This is called after syncExtra so that any Claude CLI invocations during
// marketplace/plugin setup cannot leave the file in a state that triggers
// the "Welcome to Claude Code" screen.
func patchClaudeJSON(outputDir string, extra map[string]interface{}) error {
	return generateClaudeJSON(outputDir, extra, nil)
}

// generatePiMCPConfig creates ~/.config/mcp/mcp.json for pi-mcp-adapter.
// pi-mcp-adapter reads this shared MCP config automatically on first use.
func generatePiMCPConfig(outputDir string, agentType string, mcpServers map[string]interface{}) error {
	if agentType != "pi" || len(mcpServers) == 0 {
		return nil
	}

	configDir := filepath.Join(outputDir, ".config", "mcp")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create Pi MCP config directory: %w", err)
	}

	config := map[string]interface{}{
		"mcpServers": mcpServers,
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Pi MCP config: %w", err)
	}

	configPath := filepath.Join(configDir, "mcp.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write Pi MCP config: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", configPath)
	return nil
}

// generatePiSettingsJSON merges managed Pi settings into ~/.pi/agent/settings.json.
func generatePiSettingsJSON(outputDir string, settingsJSON map[string]interface{}) error {
	if len(settingsJSON) == 0 {
		return nil
	}

	piDir := filepath.Join(outputDir, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		return fmt.Errorf("failed to create Pi agent directory: %w", err)
	}

	settingsPath := filepath.Join(piDir, "settings.json")
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("failed to parse existing Pi settings.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing Pi settings.json: %w", err)
	}

	for key, value := range settingsJSON {
		existing[key] = value
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Pi settings.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write Pi settings.json: %w", err)
	}
	if err := os.Chmod(settingsPath, 0600); err != nil {
		return fmt.Errorf("failed to set Pi settings.json permissions: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s with %d managed setting(s)", settingsPath, len(settingsJSON))
	return nil
}

// generatePiModelsJSON merges managed custom models into ~/.pi/agent/models.json.
func generatePiModelsJSON(outputDir string, modelsJSON map[string]interface{}) error {
	if len(modelsJSON) == 0 {
		return nil
	}

	piDir := filepath.Join(outputDir, ".pi", "agent")
	if err := os.MkdirAll(piDir, 0755); err != nil {
		return fmt.Errorf("failed to create Pi agent directory: %w", err)
	}

	modelsPath := filepath.Join(piDir, "models.json")
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(modelsPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("failed to parse existing Pi models.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing Pi models.json: %w", err)
	}

	mergePiModelProviders(existing, modelsJSON)
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Pi models.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(modelsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write Pi models.json: %w", err)
	}
	if err := os.Chmod(modelsPath, 0600); err != nil {
		return fmt.Errorf("failed to set Pi models.json permissions: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", modelsPath)
	return nil
}

func mergePiModelProviders(target, managed map[string]interface{}) {
	targetProviders, _ := target["providers"].(map[string]interface{})
	if targetProviders == nil {
		targetProviders = make(map[string]interface{})
		target["providers"] = targetProviders
	}
	managedProviders, _ := managed["providers"].(map[string]interface{})
	for providerName, value := range managedProviders {
		managedProvider, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		existingProvider, _ := targetProviders[providerName].(map[string]interface{})
		if existingProvider == nil {
			existingProvider = make(map[string]interface{})
		}
		for key, managedValue := range managedProvider {
			if key == "models" {
				existingProvider[key] = mergePiModels(existingProvider[key], managedValue)
				continue
			}
			existingProvider[key] = managedValue
		}
		targetProviders[providerName] = existingProvider
	}
}

func mergePiModels(existingValue, managedValue interface{}) []interface{} {
	existing, _ := existingValue.([]interface{})
	managed, _ := managedValue.([]interface{})
	result := append([]interface{}(nil), existing...)
	indexes := make(map[string]int, len(result))
	for i, value := range result {
		if model, ok := value.(map[string]interface{}); ok {
			if id, ok := model["id"].(string); ok {
				indexes[id] = i
			}
		}
	}
	for _, value := range managed {
		model, ok := value.(map[string]interface{})
		id, hasID := model["id"].(string)
		if ok && hasID {
			if index, exists := indexes[id]; exists {
				result[index] = value
				continue
			}
			indexes[id] = len(result)
		}
		result = append(result, value)
	}
	return result
}

// generateSettingsJSON creates ~/.claude/settings.json.
