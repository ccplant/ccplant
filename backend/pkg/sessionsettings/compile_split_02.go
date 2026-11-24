package sessionsettings

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func generateSettingsJSON(outputDir string, settingsJSON map[string]interface{}) error {
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")

	// If no settings provided, create minimal empty settings
	if len(settingsJSON) == 0 {
		settingsJSON = map[string]interface{}{
			"settings": map[string]interface{}{
				"mcp.enabled": true,
			},
		}
	}

	// Always set autoUpdatesChannel to "stable" to prevent automatic updates
	// to non-stable channels in session containers.
	settingsJSON["autoUpdatesChannel"] = "stable"

	data, err := json.MarshalIndent(settingsJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", settingsPath)
	return nil
}

// managedSettingsPath is the well-known location for organisation-managed Claude Code policy.
// The same file is read by the Codex hook generator to keep both runtimes in sync.
const managedSettingsPath = "/etc/claude-code/managed-settings.json"

// generateCodexHooksJSON creates ~/.codex/hooks.json for Codex CLI hook configuration.
// Only written when hooksJSON is non-empty (i.e., for codex-acp sessions).
// Hooks from managed-settings.json are merged in so the same organisation-wide policy
// (UserPromptSubmit, Stop notifications, etc.) applies to Codex sessions too.
func generateCodexHooksJSON(outputDir string, hooksJSON map[string]interface{}) error {
	if len(hooksJSON) == 0 {
		return nil
	}

	// Merge hooks from the managed-settings policy file (mirrors Claude Code behaviour).
	hooksJSON = mergeCodexManagedHooks(hooksJSON)

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")
	data, err := json.MarshalIndent(hooksJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal codex hooks.json: %w", err)
	}

	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write codex hooks.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", hooksPath)
	return nil
}

// MergeCodexManagedHooks reads managed-settings.json and appends its hook entries
// to each matching event in hooksJSON. Unknown events are added as new entries.
// Returns the original hooksJSON unchanged if the file is absent or unreadable.
// Exported so callers outside this package (e.g. the session manager) can reuse the logic.
func MergeCodexManagedHooks(hooksJSON map[string]interface{}) map[string]interface{} {
	return mergeCodexManagedHooks(hooksJSON)
}

// mergeCodexManagedHooks is the unexported implementation; call MergeCodexManagedHooks externally.
func mergeCodexManagedHooks(hooksJSON map[string]interface{}) map[string]interface{} {
	data, err := os.ReadFile(managedSettingsPath)
	if err != nil {
		return hooksJSON // file absent — nothing to merge
	}

	var managed map[string]interface{}
	if err := json.Unmarshal(data, &managed); err != nil {
		log.Printf("[COMPILE-SETTINGS] Warning: failed to parse %s: %v", managedSettingsPath, err)
		return hooksJSON
	}

	managedHooks, ok := managed["hooks"].(map[string]interface{})
	if !ok || len(managedHooks) == 0 {
		return hooksJSON
	}

	// Work on a shallow copy so we don't mutate the caller's map.
	merged := make(map[string]interface{}, len(hooksJSON))
	for k, v := range hooksJSON {
		merged[k] = v
	}

	baseHooks, ok := merged["hooks"].(map[string]interface{})
	if !ok {
		baseHooks = make(map[string]interface{})
	}
	// Clone the inner hooks map too.
	innerHooks := make(map[string]interface{}, len(baseHooks))
	for k, v := range baseHooks {
		innerHooks[k] = v
	}

	for event, entries := range managedHooks {
		managedList, ok := entries.([]interface{})
		if !ok {
			continue
		}
		if existing, ok := innerHooks[event].([]interface{}); ok {
			innerHooks[event] = append(existing, managedList...)
		} else {
			innerHooks[event] = managedList
		}
	}

	merged["hooks"] = innerHooks
	log.Printf("[COMPILE-SETTINGS] Merged %d hook event(s) from %s into codex hooks.json", len(managedHooks), managedSettingsPath)
	return merged
}

// generateCodexInstructionsMD creates ~/.codex/AGENTS.md for global Codex CLI
// instructions, equivalent to ~/.claude/CLAUDE.md for Claude Code.
// Only written when instructionsMD is non-empty; the default baked into the
// Docker image (copied by entrypoint.sh) remains when this is empty.
func generateCodexInstructionsMD(outputDir string, instructionsMD string) error {
	if instructionsMD == "" {
		return nil
	}

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	instructionsPath := filepath.Join(codexDir, "AGENTS.md")
	if err := os.WriteFile(instructionsPath, []byte(instructionsMD), 0644); err != nil {
		return fmt.Errorf("failed to write codex AGENTS.md: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", instructionsPath)
	return nil
}

// generateCodexConfigTOML creates ~/.codex/config.toml for Codex CLI configuration.
// Only written when configTOML is non-empty or OPENAI_BASE_URL is configured.
func generateCodexConfigTOML(outputDir string, configTOML string, env map[string]string, catalogEnv map[string]string) error {
	customProviderTOML := codexCustomOpenAIProviderTOML(env)
	// Profile model candidates are registered in a Codex catalog so codex-acp
	// accepts them from the ACP model switcher.
	catalogPath := generateCodexModelCatalog(outputDir, catalogEnv)
	if configTOML == "" && customProviderTOML == "" && catalogPath == "" {
		return nil
	}

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	content := appendCodexConfigSection(configTOML, customProviderTOML)
	if catalogPath != "" {
		content = appendCodexModelCatalogConfig(content, catalogPath)
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write codex config.toml: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", configPath)
	return nil
}

const codexCustomOpenAIProviderID = "agentapi_openai_compatible"

func codexCustomOpenAIProviderTOML(env map[string]string) string {
	baseURL := strings.TrimSpace(env["OPENAI_BASE_URL"])
	if baseURL == "" {
		return ""
	}
	model := codexModelFromEnv(env)

	var sb strings.Builder
	if model != "" {
		sb.WriteString("model = ")
		sb.WriteString(tomlString(model))
		sb.WriteString("\n")
	}
	metadata := codexModelMetadataFromEnv(env, model != "")
	for _, line := range metadata {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("model_provider = ")
	sb.WriteString(tomlString(codexCustomOpenAIProviderID))
	sb.WriteString("\n\n")
	sb.WriteString("[model_providers.")
	sb.WriteString(codexCustomOpenAIProviderID)
	sb.WriteString("]\n")
	sb.WriteString("name = \"OpenAI compatible\"\n")
	sb.WriteString("base_url = ")
	sb.WriteString(tomlString(baseURL))
	sb.WriteString("\n")
	sb.WriteString("wire_api = \"responses\"\n")
	if strings.TrimSpace(env["OPENAI_API_KEY"]) != "" {
		sb.WriteString("env_key = \"OPENAI_API_KEY\"\n")
	}
	return sb.String()
}

func codexModelFromEnv(env map[string]string) string {
	if model := strings.TrimSpace(env["CODEX_MODEL"]); model != "" {
		return model
	}
	return strings.TrimSpace(env["OPENAI_MODEL"])
}

func codexModelMetadataFromEnv(env map[string]string, modelConfigured bool) []string {
	metadata := []string{}

	contextWindow := strings.TrimSpace(env["CODEX_MODEL_CONTEXT_WINDOW"])
	if contextWindow == "" {
		contextWindow = strings.TrimSpace(env["OPENAI_MODEL_CONTEXT_WINDOW"])
	}
	if contextWindow == "" && modelConfigured {
		contextWindow = "128000"
	}
	if contextWindow != "" {
		metadata = append(metadata, fmt.Sprintf("model_context_window = %s", contextWindow))
	}

	autoCompactTokenLimit := strings.TrimSpace(env["CODEX_MODEL_AUTO_COMPACT_TOKEN_LIMIT"])
	if autoCompactTokenLimit == "" {
		autoCompactTokenLimit = strings.TrimSpace(env["OPENAI_MODEL_AUTO_COMPACT_TOKEN_LIMIT"])
	}
	if autoCompactTokenLimit == "" && modelConfigured {
		autoCompactTokenLimit = "64000"
	}
	if autoCompactTokenLimit != "" {
		metadata = append(metadata, fmt.Sprintf("model_auto_compact_token_limit = %s", autoCompactTokenLimit))
	}

	if supportsReasoningSummaries := strings.TrimSpace(env["CODEX_MODEL_SUPPORTS_REASONING_SUMMARIES"]); supportsReasoningSummaries != "" {
		metadata = append(metadata, fmt.Sprintf("model_supports_reasoning_summaries = %s", supportsReasoningSummaries))
	}

	return metadata
}

func appendCodexConfigSection(base string, section string) string {
	if section == "" {
		return base
	}
	base = removeTopLevelTOMLKey(base, "model_provider")
	selector, provider := splitCodexCustomProviderTOML(section)
	if topLevelTOMLKeyIsSet(selector, "model") {
		base = removeTopLevelTOMLKey(base, "model")
	}
	for _, key := range []string{"model_context_window", "model_auto_compact_token_limit", "model_supports_reasoning_summaries"} {
		if topLevelTOMLKeyIsSet(selector, key) {
			base = removeTopLevelTOMLKey(base, key)
		}
	}
	if base == "" {
		return section
	}

	var sb strings.Builder
	sb.WriteString(selector)
	if !strings.HasSuffix(selector, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(base)
	if !strings.HasSuffix(base, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(provider)
	return sb.String()
}

func splitCodexCustomProviderTOML(section string) (string, string) {
	parts := strings.SplitN(section, "\n\n", 2)
	if len(parts) != 2 {
		return section, ""
	}
	return parts[0] + "\n", parts[1]
}

func topLevelTOMLKeyIsSet(content string, key string) bool {
	if content == "" {
		return false
	}

	inTopLevel := true
	prefix := key + " "
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTopLevel = false
		}
		if inTopLevel && (strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, key+"=")) {
			return true
		}
	}
	return false
}

func removeTopLevelTOMLKey(content string, key string) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inTopLevel := true
	prefix := key + " "
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTopLevel = false
		}
		if inTopLevel && (strings.HasPrefix(trimmed, prefix) || strings.HasPrefix(trimmed, key+"=")) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func tomlString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(b)
}

// generateCodexMCPServers appends MCP server entries to ~/.codex/config.toml using
// the [mcp_servers.<name>] nested-table format expected by the Codex CLI.
// Only appends when mcpServers is non-empty; the file is created if absent.
// The input map mirrors the ClaudeConfig.MCPServers format (name → config map).
