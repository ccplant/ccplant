package provisioner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) buildAgentCommand(settings *sessionsettings.SessionSettings, envMap map[string]string) (string, []string) {
	agentType := settings.Session.AgentType
	agentapiProxyBinary := proxybinary.FromMap(envMap)

	agentapiPort := os.Getenv("AGENTAPI_PORT")
	if agentapiPort == "" {
		agentapiPort = "8080"
	}
	agentapiHost := strings.TrimSpace(envMap["AGENTAPI_BIND_HOST"])
	if agentapiHost == "" {
		agentapiHost = "0.0.0.0"
	}
	listenArgs := []string{"--host", agentapiHost, "--port", agentapiPort}
	if socket := strings.TrimSpace(envMap["AGENTAPI_LOCAL_SOCKET"]); socket != "" {
		listenArgs = []string{"--socket", socket}
	}

	switch agentType {
	case "claude-acp":
		// Start the acp-server bridge that wraps claude-agent-acp (ACP agent) via stdio.
		// The bridge exposes an agentapi-compatible HTTP server on AGENTAPI_PORT.
		// --output-file writes conversation history in acp-posts JSONL format for Slack integration.
		// claude-agent-acp is the official ACP adapter for the Claude Agent SDK:
		// https://github.com/agentclientprotocol/claude-agent-acp
		args := append([]string{"acp-server"}, listenArgs...)
		return agentapiProxyBinary, append(args, []string{
			"--history-file", filepath.Join(runtimeHome, ".session", "acp-history.jsonl"),
			"--output-file", acpHistoryPath,
			"--",
			"claude-agent-acp",
		}...)

	case "codex-acp":
		// Start the acp-server bridge that wraps codex-acp (ACP adapter for OpenAI Codex) via stdio.
		// https://github.com/agentclientprotocol/codex-acp
		// --auto-approve bypasses the UI permission modal at the ACP bridge layer.
		adapterCommand := []string{"codex-acp"}
		if strings.TrimSpace(os.Getenv("CCPLANT_EXECUTOR_REQUIRED")) == "1" || strings.TrimSpace(envMap["CCPLANT_EXECUTOR_REQUIRED"]) == "1" {
			// The executor replaces /bin/bash in the agent container. Run this
			// control-plane adapter through the image's JavaScript runtime
			// directly, so a wrapper shebang can never move codex-acp (and its
			// app-server child) into the executor sidecar. Shells spawned by the
			// agent still resolve to the executor client as intended.
			adapterCommand = []string{containerJavaScriptRuntime, containerCodexACPEntrypoint}
		}
		args := append([]string{"acp-server"}, listenArgs...)
		args = append(args,
			"--history-file", filepath.Join(runtimeHome, ".session", "acp-history.jsonl"),
			"--auto-approve",
			"--",
		)
		return agentapiProxyBinary, append(args, adapterCommand...)

	case "pi":
		// Start the acp-server bridge that wraps pi-acp via stdio. pi-acp starts
		// `pi --mode rpc`; provider configuration is generated from the
		// selected compatible API connection.
		// https://github.com/svkozak/pi-acp
		ensurePiEnv(envMap)
		args := append([]string{"acp-server"}, listenArgs...)
		return agentapiProxyBinary, append(args, []string{
			"--auto-approve",
			"--",
			"pi-acp",
		}...)

	case "cursor":
		// Start the acp-server bridge that wraps Cursor Agent CLI's native ACP server via stdio.
		// https://cursor.com/docs/cli/acp
		// --auto-approve bypasses the UI permission modal at the ACP bridge layer.
		args := append([]string{"acp-server"}, listenArgs...)
		return agentapiProxyBinary, append(args, []string{
			"--auto-approve",
			"--raw-json-log",
			"--",
			"agent", "acp",
		}...)

	case "antigravity":
		// Start the bridge around Antigravity CLI's native ACP v1 server. The
		// session container is the permission boundary, so both bridge and CLI
		// approval prompts are disabled for unattended operation.
		args := append([]string{"acp-server"}, listenArgs...)
		return agentapiProxyBinary, append(args, []string{
			"--auto-approve",
			"--",
			"agy", "--dangerously-skip-permissions", "acp",
		}...)

	default:
		// Default: agentapi server wrapping claude
		claudeCmd := "claude"
		if claudeArgs := os.Getenv("CLAUDE_ARGS"); claudeArgs != "" {
			claudeCmd = claudeCmd + " " + claudeArgs
		}
		return "agentapi", []string{
			"server",
			"--host", agentapiHost,
			"--allowed-hosts", "*",
			"--allowed-origins", "*",
			"--port", agentapiPort,
			"--",
			"sh", "-c", claudeCmd,
		}
	}
}

func ensurePiEnv(envMap map[string]string) {
	if envMap == nil {
		return
	}
	if strings.TrimSpace(envMap["PI_ACP_PI_COMMAND"]) == "" {
		envMap["PI_ACP_PI_COMMAND"] = "pi"
	}
}

func writePiAgentInstructions(sourcePath, destPath string) error {
	return copyAgentInstructions(sourcePath, destPath, "Pi")
}

func copyAgentInstructions(sourcePath, destPath, agentName string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("source and destination paths must be set")
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read source instructions: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s agent dir: %w", agentName, err)
	}
	if err := os.WriteFile(destPath, content, 0o644); err != nil {
		return fmt.Errorf("write %s instructions: %w", agentName, err)
	}
	log.Printf("[PROVISIONER] Wrote %s instructions to %s", agentName, destPath)
	return nil
}

func writeAntigravitySettings(settingsPath, workspace string) error {
	if strings.TrimSpace(settingsPath) == "" || strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("settings path and workspace must be set")
	}
	settingsJSON := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &settingsJSON); err != nil {
			return fmt.Errorf("parse existing Antigravity settings: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing Antigravity settings: %w", err)
	}
	settingsJSON["enableTerminalSandbox"] = false
	settingsJSON["toolPermission"] = "always-proceed"
	settingsJSON["trustedWorkspaces"] = []string{workspace}
	return writeJSONFile(settingsPath, settingsJSON, "Antigravity settings")
}

func writeAntigravityMCPConfig(configPath string, mcpServers map[string]interface{}) error {
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("MCP config path must be set")
	}
	servers := make(map[string]interface{}, len(mcpServers))
	for name, raw := range mcpServers {
		config, ok := raw.(map[string]interface{})
		if !ok {
			servers[name] = raw
			continue
		}
		normalized := make(map[string]interface{}, len(config))
		for key, value := range config {
			if key != "type" {
				normalized[key] = value
			}
		}
		if _, exists := normalized["serverUrl"]; !exists {
			if remoteURL, ok := normalized["url"]; ok {
				normalized["serverUrl"] = remoteURL
				delete(normalized, "url")
			}
		}
		servers[name] = normalized
	}
	return writeJSONFile(configPath, map[string]interface{}{"mcpServers": servers}, "Antigravity MCP config")
}

func writeJSONFile(path string, value interface{}, label string) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", label, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	log.Printf("[PROVISIONER] Wrote %s to %s", label, path)
	return nil
}

func piMCPServers(settings *sessionsettings.SessionSettings) map[string]interface{} {
	if settings == nil {
		return nil
	}
	if len(settings.Codex.MCPServers) > 0 {
		return settings.Codex.MCPServers
	}
	if len(settings.Claude.MCPServers) > 0 {
		return settings.Claude.MCPServers
	}
	return nil
}

func writePiSettings(settingsPath string, mcpServers map[string]interface{}) error {
	if strings.TrimSpace(settingsPath) == "" {
		return fmt.Errorf("settings path must be set")
	}

	settingsJSON := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &settingsJSON); err != nil {
			return fmt.Errorf("parse existing Pi settings: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing Pi settings: %w", err)
	}

	// Session containers are already isolated by agentapi-proxy. Trust project
	// resources automatically so pi-acp's non-interactive RPC process can load
	// repository-local settings, skills, and extensions without a prompt.
	settingsJSON["defaultProjectTrust"] = "always"
	if len(mcpServers) > 0 {
		settingsJSON["mcpServers"] = mcpServers
	}

	data, err := json.MarshalIndent(settingsJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Pi settings: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("create Pi settings dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("write Pi settings: %w", err)
	}
	log.Printf("[PROVISIONER] Wrote Pi settings to %s", settingsPath)
	return nil
}

// waitForAgentAPI polls agentapiURL/status until it responds 200 or the
// context is cancelled.  maxRetries × 0.5 s = total wait time.
