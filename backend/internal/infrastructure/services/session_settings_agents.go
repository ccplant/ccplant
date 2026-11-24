package services

import (
	"fmt"
	"log"

	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) agentConfiguration() {
	materialized := b.materialized
	req := b.request
	session := b.session
	settings := b.settings
	// Claude config
	settingsJSON := materialized.SettingsJSON

	// Always inject a cycle Stop hook so that writing /tmp/check/CYCLE_ENABLED at
	// any point activates cycling without requiring a session restart.
	// The hook is a no-op when CYCLE_ENABLED does not exist (the cycle command exits
	// immediately with "CYCLE_ENABLED not found"), so it is safe to register
	// unconditionally.
	// The hook runs the cycle command in background (nohup ... &) so that it exits
	// immediately without blocking Claude Code's hook runner.
	{
		// The cycle message is stored in /tmp/check/CYCLE_ENABLED (written by the
		// provisioner via settings.Files below when CycleMessage is set).
		// The cycle command reads the message directly from that file.
		innerCmd := proxybinary.ShellReference() + " client cycle"
		if req.CycleMaxCount > 0 {
			innerCmd += fmt.Sprintf(" --max-count %d", req.CycleMaxCount)
		}
		// Wrap with nohup + background so the hook exits immediately (exit 0)
		// and the actual wait+send happens asynchronously after all hooks complete.
		command := fmt.Sprintf("nohup %s >> /tmp/cycle.log 2>&1 &", innerCmd)

		cycleHookEntry := map[string]interface{}{
			"hooks": []map[string]interface{}{
				{"type": "command", "command": command},
			},
		}

		if settingsJSON == nil {
			settingsJSON = make(map[string]interface{})
		}

		hooksMap, ok := settingsJSON["hooks"].(map[string]interface{})
		if !ok {
			hooksMap = make(map[string]interface{})
		}

		// Merge with existing Stop hooks (e.g., from oneshot settings)
		if existing, ok := hooksMap["Stop"].([]interface{}); ok {
			hooksMap["Stop"] = append(existing, cycleHookEntry)
		} else {
			hooksMap["Stop"] = []interface{}{cycleHookEntry}
		}

		settingsJSON["hooks"] = hooksMap
		log.Printf("[K8S_SESSION] Injected cycle Stop hook for session %s (max-count=%d)", session.id, req.CycleMaxCount)
	}

	settings.Claude = sessionsettings.ClaudeConfig{
		ClaudeJSON: map[string]interface{}{
			"hasCompletedOnboarding":        true,
			"bypassPermissionsModeAccepted": true,
		},
		SettingsJSON: settingsJSON,
		MCPServers:   materialized.MCPServers,
	}

	// For Codex ACP sessions, populate Codex-specific config.
	switch req.AgentType {
	case "codex-acp":
		settings.Codex = sessionsettings.CodexConfig{
			HooksJSON: buildCodexHooksJSON(settingsJSON),
			// Bypass permission prompts and disable Codex's own sandbox.
			// agentapi-proxy provides its own sandbox, so Codex's bubblewrap-based
			// sandbox is redundant and causes spurious permission requests.
			ConfigTOML: "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\ncheck_for_update_on_startup = false\n",
			MCPServers: materialized.MCPServers,
		}
		log.Printf("[K8S_SESSION] Injected Codex hooks and set INITIAL_AGENT_MODE=agent-full-access, approval-mode=full-auto, sandbox_mode=danger-full-access, check_for_update_on_startup=false for session %s", session.id)
	}
}

func (b *sessionSettingsBuilder) startupCommand() {
	env := b.environment
	m := b.manager
	req := b.request
	settings := b.settings
	settingsJSON := settings.Claude.SettingsJSON
	// Startup command (simplified version for now - full command logic in pod)
	switch req.AgentType {
	case "claude-acp":
		// acp-server bridges claude-agent-acp (ACP over stdio) to the agentapi HTTP interface.
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{proxybinary.FromMap(env)},
			Args: []string{
				"acp-server",
				"--port", fmt.Sprintf("%d", m.k8sConfig.BasePort),
				"--",
				"claude-agent-acp",
			},
		}
		// Bypass permission prompts for claude-acp sessions so tool calls
		// proceed without waiting for user approval.
		if settingsJSON == nil {
			settingsJSON = make(map[string]interface{})
		}
		settingsJSON["permissions"] = map[string]interface{}{
			"defaultMode": "bypassPermissions",
		}
	case "codex-acp":
		// acp-server bridges codex-acp (ACP adapter for OpenAI Codex) to the agentapi HTTP interface.
		// https://github.com/agentclientprotocol/codex-acp
		// --auto-approve bypasses the UI permission modal at the ACP bridge layer.
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{proxybinary.FromMap(env)},
			Args: []string{
				"acp-server",
				"--port", fmt.Sprintf("%d", m.k8sConfig.BasePort),
				"--auto-approve",
				"--",
				"codex-acp",
			},
		}
	case "pi":
		// acp-server bridges pi-acp to Pi. Providers are generated from the
		// selected compatible API connection.
		// https://github.com/svkozak/pi-acp
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{proxybinary.FromMap(env)},
			Args: []string{
				"acp-server",
				"--port", fmt.Sprintf("%d", m.k8sConfig.BasePort),
				"--auto-approve",
				"--",
				"pi-acp",
			},
			PreScript: piInstallPreScript + `
if [ -n "${CCPLANT_PI_PACKAGES:-}" ]; then
  printf '%s\n' "$CCPLANT_PI_PACKAGES" | while IFS= read -r package; do
    [ -n "$package" ] && pi install "$package"
  done
fi`,
		}
	case "cursor":
		// acp-server bridges Cursor Agent CLI's native ACP server to the agentapi HTTP interface.
		// https://cursor.com/docs/cli/acp
		// --auto-approve bypasses the UI permission modal at the ACP bridge layer.
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{proxybinary.FromMap(env)},
			Args: []string{
				"acp-server",
				"--port", fmt.Sprintf("%d", m.k8sConfig.BasePort),
				"--auto-approve",
				"--raw-json-log",
				"--",
				"agent", "acp",
			},
		}
	case "antigravity":
		// Antigravity CLI exposes its native ACP v1 server over stdio.
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{proxybinary.FromMap(env)},
			Args: []string{
				"acp-server",
				"--port", fmt.Sprintf("%d", m.k8sConfig.BasePort),
				"--auto-approve",
				"--",
				"agy", "acp",
			},
		}
	default:
		settings.Startup = sessionsettings.StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--allowed-hosts", "*", "--allowed-origins", "*", "--port", fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		}
	}
	settings.Startup.CommandWrapperTemplate = req.ProfileCommandWrapper
}
