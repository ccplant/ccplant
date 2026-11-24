package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// TestWriteWebhookPayloadFile_WritesWhenAbsent verifies that
// writeWebhookPayloadFile creates the file with the given payload when
// webhookPayloadPath does not exist (stock-session case).
func TestWriteWebhookPayloadFile_WritesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opt", "webhook", "payload.json")

	// Override the package-level constant for the duration of this test.
	origPath := webhookPayloadPath
	// webhookPayloadPath is a const, so we use a helper that accepts the path.
	// Call the internal logic directly via the exported-for-test variant.
	// Since the function is unexported, we test it by temporarily overriding
	// the path via a wrapper test helper.
	_ = origPath // suppress unused warning

	writeWebhookPayloadFileToPath(path, `{"event":"push"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created at %s, got error: %v", path, err)
	}
	if string(got) != `{"event":"push"}` {
		t.Errorf("expected payload %q, got %q", `{"event":"push"}`, string(got))
	}
}

func TestBuildAgentCommandCursor(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "cursor"},
	}, nil)

	if cmd != "ccplant" {
		t.Fatalf("command = %q, want ccplant", cmd)
	}
	want := []string{"acp-server", "--host", "0.0.0.0", "--port", "9000", "--auto-approve", "--raw-json-log", "--", "agent", "acp"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildAgentCommandUsesConfiguredLoopbackHost(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")

	_, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "codex-acp"},
	}, map[string]string{"AGENTAPI_BIND_HOST": "127.0.0.1"})

	wantPrefix := []string{"acp-server", "--host", "127.0.0.1", "--port", "9000"}
	if len(args) < len(wantPrefix) || !reflect.DeepEqual(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %#v, want %#v", args, wantPrefix)
	}
}

func TestBuildAgentCommandUsesUnixSocketWithoutTCPListener(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	socketPath := filepath.Join(t.TempDir(), "agent.sock")

	_, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "codex-acp"},
	}, map[string]string{"AGENTAPI_LOCAL_SOCKET": socketPath})

	wantPrefix := []string{"acp-server", "--socket", socketPath}
	if len(args) < len(wantPrefix) || !reflect.DeepEqual(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %#v, want %#v", args, wantPrefix)
	}
	for _, arg := range args {
		if arg == "--host" || arg == "--port" {
			t.Fatalf("Unix socket command unexpectedly contains TCP flag %q: %#v", arg, args)
		}
	}
}

func TestUnixOnlyAgentTypesFailClosedForLegacyRuntime(t *testing.T) {
	for _, agentType := range []string{"claude-acp", "codex-acp", "pi", "cursor", "antigravity"} {
		if !usesACPUnixSocket(agentType) {
			t.Fatalf("usesACPUnixSocket(%q) = false", agentType)
		}
	}
	for _, agentType := range []string{"", "claude", "claude-legacy"} {
		if usesACPUnixSocket(agentType) {
			t.Fatalf("usesACPUnixSocket(%q) = true, want fail-closed", agentType)
		}
	}
}

func TestBuildAgentCommandUsesConfiguredProxyBinary(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	env := map[string]string{"CCPLANT_BINARY_PATH": "/Applications/agentapi-proxy.app/Contents/MacOS/agentapi-proxy"}

	cmd, _ := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "codex-acp"},
	}, env)

	if cmd != env["CCPLANT_BINARY_PATH"] {
		t.Fatalf("command = %q, want bundled binary %q", cmd, env["CCPLANT_BINARY_PATH"])
	}
}

func TestBuildAgentCommandKeepsCodexACPInAgentContainer(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	t.Setenv("CCPLANT_EXECUTOR_REQUIRED", "1")
	env := map[string]string{}

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "codex-acp"},
	}, env)

	if cmd != "ccplant" {
		t.Fatalf("command = %q, want ccplant", cmd)
	}
	want := []string{"acp-server", "--host", "0.0.0.0", "--port", "9000", "--history-file", filepath.Join(runtimeHome, ".session", "acp-history.jsonl"), "--auto-approve", "--", containerJavaScriptRuntime, containerCodexACPEntrypoint}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestNormalizeNativeSettingsUsesManagedBinary(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", root)
	t.Setenv("HOME", home)
	t.Setenv("CCPLANT_BINARY_PATH", "/usr/local/libexec/agentapi-proxy/agentapi-proxy")
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "pi"},
		Env: map[string]string{
			"CCPLANT_BINARY_PATH": "ccplant",
			"PI_ACP_PI_COMMAND":   "/home/agentapi/.session/pi-pi",
		},
	}

	normalizeNativeSettings(settings)

	if got := settings.Env["CCPLANT_BINARY_PATH"]; got != "/usr/local/libexec/agentapi-proxy/agentapi-proxy" {
		t.Fatalf("CCPLANT_BINARY_PATH = %q", got)
	}
	if got := settings.Env["PI_ACP_PI_COMMAND"]; got != "pi" {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
}

func TestBuildAgentCommandPi(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	env := map[string]string{"OPENAI_API_KEY": "openai-key"}

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "pi"},
	}, env)

	if cmd != "ccplant" {
		t.Fatalf("command = %q, want ccplant", cmd)
	}
	want := []string{"acp-server", "--host", "0.0.0.0", "--port", "9000", "--auto-approve", "--", "pi-acp"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if _, ok := env["OLLAMA_API_KEY"]; ok {
		t.Fatalf("OLLAMA_API_KEY should not be synthesized")
	}
	if got := env["PI_ACP_PI_COMMAND"]; got != "pi" {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
}

func TestBuildAgentCommandAntigravity(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")

	cmd, args := (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "antigravity"},
	}, map[string]string{})

	if cmd != "ccplant" {
		t.Fatalf("command = %q, want ccplant", cmd)
	}
	want := []string{"acp-server", "--host", "0.0.0.0", "--port", "9000", "--auto-approve", "--", "agy", "--dangerously-skip-permissions", "acp"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestBuildAgentCommandPiPreservesExplicitModel(t *testing.T) {
	t.Setenv("AGENTAPI_PORT", "9000")
	env := map[string]string{
		"PI_ACP_PI_COMMAND": "/custom/pi-wrapper",
		"PI_MODEL":          "ollama-cloud/gpt-oss:120b",
	}

	_, _ = (&Server{}).buildAgentCommand(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{AgentType: "pi"},
	}, env)

	if got := env["PI_ACP_PI_COMMAND"]; got != "/custom/pi-wrapper" {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
}

func TestWritePiAgentInstructionsCopiesClaudeMD(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, ".claude", "CLAUDE.md")
	destPath := filepath.Join(dir, ".pi", "agent", "AGENTS.md")
	content := "# Instructions\n\nUse the repo conventions.\n"
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := writePiAgentInstructions(sourcePath, destPath); err != nil {
		t.Fatalf("writePiAgentInstructions: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read Pi AGENTS.md: %v", err)
	}
	if string(got) != content {
		t.Fatalf("Pi AGENTS.md = %q, want %q", string(got), content)
	}
}

func TestWritePiAgentInstructionsSkipsMissingSource(t *testing.T) {
	destPath := filepath.Join(t.TempDir(), ".pi", "agent", "AGENTS.md")

	if err := writePiAgentInstructions("/does/not/exist/CLAUDE.md", destPath); err != nil {
		t.Fatalf("writePiAgentInstructions should skip missing source: %v", err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatalf("Pi AGENTS.md should not be created, stat err=%v", err)
	}
}

func TestWritePiSettingsMergesSettings(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	existing := map[string]interface{}{
		"defaultProvider": "ollama-cloud",
		"theme":           "dark",
	}
	existingData, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(settingsPath, existingData, 0o644); err != nil {
		t.Fatalf("write existing settings: %v", err)
	}

	servers := map[string]interface{}{
		"github": map[string]interface{}{
			"type":    "stdio",
			"command": "github-mcp-server",
			"args":    []interface{}{"stdio"},
			"env": map[string]interface{}{
				"GITHUB_TOKEN": "token",
			},
		},
	}
	if err := writePiSettings(settingsPath, servers); err != nil {
		t.Fatalf("writePiSettings: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if got["defaultProvider"] != "ollama-cloud" || got["theme"] != "dark" {
		t.Fatalf("existing settings not preserved: %#v", got)
	}
	if got["defaultProjectTrust"] != "always" {
		t.Fatalf("defaultProjectTrust = %#v, want always", got["defaultProjectTrust"])
	}
	gotServers, ok := got["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers = %T, want map", got["mcpServers"])
	}
	if _, ok := gotServers["github"]; !ok {
		t.Fatalf("github MCP server missing: %#v", gotServers)
	}
}

func TestWritePiSettingsEnablesProjectTrustWithoutMCPServers(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), ".pi", "agent", "settings.json")

	if err := writePiSettings(settingsPath, nil); err != nil {
		t.Fatalf("writePiSettings: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if got["defaultProjectTrust"] != "always" {
		t.Fatalf("defaultProjectTrust = %#v, want always", got["defaultProjectTrust"])
	}
	if _, ok := got["mcpServers"]; ok {
		t.Fatalf("mcpServers should be omitted when none are configured: %#v", got)
	}
}

func TestPiMCPServersPrefersCodexThenClaude(t *testing.T) {
	claudeServers := map[string]interface{}{"claude": map[string]interface{}{"type": "http", "url": "https://claude.example"}}
	codexServers := map[string]interface{}{"codex": map[string]interface{}{"type": "http", "url": "https://codex.example"}}

	got := piMCPServers(&sessionsettings.SessionSettings{
		Claude: sessionsettings.ClaudeConfig{MCPServers: claudeServers},
		Codex:  sessionsettings.CodexConfig{MCPServers: codexServers},
	})
	if !reflect.DeepEqual(got, codexServers) {
		t.Fatalf("piMCPServers should prefer Codex MCP servers, got %#v", got)
	}

	got = piMCPServers(&sessionsettings.SessionSettings{
		Claude: sessionsettings.ClaudeConfig{MCPServers: claudeServers},
	})
	if !reflect.DeepEqual(got, claudeServers) {
		t.Fatalf("piMCPServers should fall back to Claude MCP servers, got %#v", got)
	}
}

func TestWriteAntigravitySettingsDisablesPromptsAndSandbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gemini", "antigravity-cli", "settings.json")
	if err := writeAntigravitySettings(path, "/workspace/repo"); err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &got) != nil {
		t.Fatalf("read settings: %v", err)
	}
	if got["enableTerminalSandbox"] != false || got["toolPermission"] != "always-proceed" {
		t.Fatalf("permission settings = %#v", got)
	}
	wantTrust := []interface{}{interface{}("/workspace/repo")}
	if !reflect.DeepEqual(got["trustedWorkspaces"], wantTrust) {
		t.Fatalf("trustedWorkspaces = %#v", got["trustedWorkspaces"])
	}
}
