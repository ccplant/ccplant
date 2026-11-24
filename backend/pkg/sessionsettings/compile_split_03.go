package sessionsettings

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mcputil "github.com/takutakahashi/agentapi-proxy/pkg/mcp"
)

func generateCodexMCPServers(outputDir string, sessionEnv map[string]string, mcpServers map[string]interface{}) error {
	if len(mcpServers) == 0 {
		return nil
	}

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")

	// Read existing content so we can append rather than overwrite.
	var existingContent string
	if data, err := os.ReadFile(configPath); err == nil {
		existingContent = string(data)
	}

	var sb strings.Builder
	if existingContent != "" {
		sb.WriteString(existingContent)
		if !strings.HasSuffix(existingContent, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	for i, name := range sortedMapKeys(mcpServers) {
		if i > 0 {
			sb.WriteString("\n")
		}
		config, ok := mcpServers[name].(map[string]interface{})
		if !ok {
			log.Printf("[COMPILE-SETTINGS] Warning: skipping MCP server %q: value is not a map", name)
			continue
		}
		writeCodexMCPServer(&sb, name, config, codexMCPEnvValues(sessionEnv, config))
	}

	if err := os.WriteFile(configPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write codex config.toml with MCP servers: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Appended %d MCP server(s) to %s", len(mcpServers), configPath)
	return nil
}

func sortedMapKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeCodexMCPServer(sb *strings.Builder, name string, config map[string]interface{}, envValues map[string]string) {
	fmt.Fprintf(sb, "[mcp_servers.%s]\n", name)
	serverType, _ := config["type"].(string)
	if _, ok := config["type"].(string); ok {
		fmt.Fprintf(sb, "type = %q\n", serverType)
	}
	for _, field := range []string{"url", "command"} {
		value, _ := config[field].(string)
		writeCodexStringField(sb, field, value, envValues)
	}
	writeCodexMCPArgs(sb, config, envValues)
	if codexMCPServerSupportsEnv(serverType) {
		writeCodexMCPMap(sb, "env", config["env"], envValues)
	}
	if codexMCPServerSupportsHTTPHeaders(serverType) {
		writeCodexMCPMap(sb, "http_headers", config["headers"], envValues)
	}
}

func writeCodexStringField(sb *strings.Builder, key, value string, envValues map[string]string) {
	if value == "" {
		return
	}
	value = mcputil.ExpandEnvVarsWithMap(value, envValues)
	fmt.Fprintf(sb, "%s = %q\n", key, value)
}

func writeCodexMCPArgs(sb *strings.Builder, config map[string]interface{}, envValues map[string]string) {
	args, _ := config["args"].([]interface{})
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if value, ok := arg.(string); ok {
			parts = append(parts, fmt.Sprintf("%q", mcputil.ExpandEnvVarsWithMap(value, envValues)))
		}
	}
	if len(args) > 0 {
		fmt.Fprintf(sb, "args = [%s]\n", strings.Join(parts, ", "))
	}
}

func writeCodexMCPMap(sb *strings.Builder, outputKey string, raw interface{}, envValues map[string]string) {
	values, _ := raw.(map[string]interface{})
	parts := make([]string, 0, len(values))
	for _, key := range sortedMapKeys(values) {
		if value, ok := values[key].(string); ok {
			if outputKey == "http_headers" {
				key = fmt.Sprintf("%q", key)
			}
			parts = append(parts, fmt.Sprintf("%s = %q", key, mcputil.ExpandEnvVarsWithMap(value, envValues)))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(sb, "%s = {%s}\n", outputKey, strings.Join(parts, ", "))
	}
}

func codexMCPEnvValues(sessionEnv map[string]string, config map[string]interface{}) map[string]string {
	env, _ := config["env"].(map[string]interface{})
	values := make(map[string]string, len(sessionEnv)+len(env))
	for k, v := range sessionEnv {
		values[k] = v
	}
	for k, v := range env {
		if s, ok := v.(string); ok {
			values[k] = mcputil.ExpandEnvVarsWithMap(s, values)
		}
	}
	return values
}

func codexMCPServerSupportsEnv(serverType string) bool {
	switch serverType {
	case "http", "streamable_http":
		return false
	default:
		return true
	}
}

func codexMCPServerSupportsHTTPHeaders(serverType string) bool {
	switch serverType {
	case "http", "streamable_http", "sse":
		return true
	default:
		return false
	}
}

// generateStartupScript creates executable shell script with the startup command.
func generateStartupScript(startupPath string, startup StartupConfig) error {
	// Create parent directory if needed
	startupDir := filepath.Dir(startupPath)
	if err := os.MkdirAll(startupDir, 0755); err != nil {
		return fmt.Errorf("failed to create startup script directory: %w", err)
	}

	// Build command line
	var commandLine string
	if len(startup.Command) > 0 {
		parts := append([]string{}, startup.Command...)
		parts = append(parts, startup.Args...)
		// Quote parts that contain spaces
		for i, part := range parts {
			if strings.ContainsAny(part, " \t\n") {
				parts[i] = fmt.Sprintf(`"%s"`, part)
			}
		}
		commandLine = strings.Join(parts, " ")
	} else {
		commandLine = "# No startup command configured"
	}

	var preScriptSection string
	if startup.PreScript != "" {
		preScriptSection = startup.PreScript + "\n\n"
	}

	scriptContent := fmt.Sprintf(`#!/bin/sh
# Generated startup script for session
# DO NOT EDIT - this file is auto-generated by compile-settings

set -e

%s%s
`, preScriptSection, commandLine)

	if err := os.WriteFile(startupPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", startupPath)
	return nil
}
