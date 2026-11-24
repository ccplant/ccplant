package sessionsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_CodexMCPServers_AppendsMcpServersToConfigToml(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-mcp",
			UserID:    "user-codex-mcp",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Codex: CodexConfig{
			MCPServers: map[string]interface{}{
				"github": map[string]interface{}{
					"type":    "stdio",
					"command": "npx",
					"args":    []interface{}{"-y", "@modelcontextprotocol/server-github"},
					"env":     map[string]interface{}{"GITHUB_TOKEN": "ghp_xxx"},
				},
				"slack": map[string]interface{}{
					"type":    "http",
					"url":     "https://mcp.example.com/slack",
					"env":     map[string]interface{}{"SLACK_TOKEN": "xoxb-token"},
					"headers": map[string]interface{}{"Authorization": "Bearer token", "X-Custom": "value"},
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	assert.FileExists(t, configPath)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	// Both servers should appear as [mcp_servers.<name>] nested-table entries.
	assert.Contains(t, content, "[mcp_servers.github]")
	assert.Contains(t, content, `type = "stdio"`)
	assert.Contains(t, content, `command = "npx"`)
	assert.Contains(t, content, "[mcp_servers.slack]")
	assert.Contains(t, content, `type = "http"`)
	assert.Contains(t, content, `url = "https://mcp.example.com/slack"`)
	assert.Contains(t, content, `http_headers = {"Authorization" = "Bearer token", "X-Custom" = "value"}`)
	assert.Contains(t, content, "GITHUB_TOKEN")
	assert.NotContains(t, content, "SLACK_TOKEN")
}

func TestCompile_CodexMCPServers_OmitsEnvForStreamableHttpMcpServers(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-mcp-streamable-http",
			UserID:    "user-codex-mcp-streamable-http",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Codex: CodexConfig{
			MCPServers: map[string]interface{}{
				"github": map[string]interface{}{
					"type": "streamable_http",
					"url":  "https://api.githubcopilot.com/mcp",
					"env":  map[string]interface{}{"GITHUB_TOKEN": "$GITHUB_TOKEN"},
					"headers": map[string]interface{}{
						"Authorization": "Bearer token",
					},
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "[mcp_servers.github]")
	assert.Contains(t, content, `type = "streamable_http"`)
	assert.Contains(t, content, `url = "https://api.githubcopilot.com/mcp"`)
	assert.Contains(t, content, `http_headers = {"Authorization" = "Bearer token"}`)
	assert.NotContains(t, content, "GITHUB_TOKEN")
	assert.NotContains(t, content, "env =")
}

func TestCompile_CodexMCPServers_ExpandsEnvPlaceholdersForHttpMcpServersWithoutEmittingEnv(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-mcp-http-env-placeholders",
			UserID:    "user-codex-mcp-http-env-placeholders",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Codex: CodexConfig{
			MCPServers: map[string]interface{}{
				"github": map[string]interface{}{
					"type": "http",
					"url":  "https://api.example.com/${MCP_TENANT:-default}/mcp",
					"env": map[string]interface{}{
						"GITHUB_TOKEN": "ghp_secret",
						"MCP_TENANT":   "acme",
					},
					"headers": map[string]interface{}{
						"Authorization": "Bearer ${GITHUB_TOKEN}",
						"X-Tenant":      "${MCP_TENANT}",
					},
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `url = "https://api.example.com/acme/mcp"`)
	assert.Contains(t, content, `http_headers = {"Authorization" = "Bearer ghp_secret", "X-Tenant" = "acme"}`)
	assert.NotContains(t, content, "env =")
	assert.NotContains(t, content, "${GITHUB_TOKEN}")
	assert.NotContains(t, content, "${MCP_TENANT}")
}

func TestCompile_CodexMCPServers_ExpandsSessionEnvPlaceholdersForHttpMcpServers(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-mcp-session-env",
			UserID:    "user-codex-mcp-session-env",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Env: map[string]string{
			"MCP_TENANT":       "session-tenant",
			"GITHUB_MCP_TOKEN": "session-token",
		},
		Codex: CodexConfig{
			MCPServers: map[string]interface{}{
				"github": map[string]interface{}{
					"type": "http",
					"url":  "https://${MCP_TENANT}.example.com/mcp",
					"headers": map[string]interface{}{
						"Authorization": "Bearer ${GITHUB_MCP_TOKEN}",
					},
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(outputDir, ".codex/config.toml"))
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `url = "https://session-tenant.example.com/mcp"`)
	assert.Contains(t, content, `http_headers = {"Authorization" = "Bearer session-token"}`)
	assert.NotContains(t, content, "${MCP_TENANT}")
	assert.NotContains(t, content, "${GITHUB_MCP_TOKEN}")
}

func TestCompile_CodexMCPServers_AppendsMcpServersAfterExistingConfigTOML(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-mcp-append",
			UserID:    "user-codex-mcp-append",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Codex: CodexConfig{
			ConfigTOML: "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n",
			MCPServers: map[string]interface{}{
				"myhub": map[string]interface{}{
					"type": "http",
					"url":  "https://myhub.example.com/mcp",
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	// Base config should be present.
	assert.Contains(t, content, "approval-mode")
	assert.Contains(t, content, "sandbox_mode")
	// MCP server should be appended after the base config.
	assert.Contains(t, content, "[mcp_servers.myhub]")
	assert.Contains(t, content, `url = "https://myhub.example.com/mcp"`)
	// The base config should appear BEFORE the mcp_servers section.
	assert.Less(t, strings.Index(content, "approval-mode"), strings.Index(content, "[mcp_servers."))
}

func TestCompile_CodexMCPServers_SkipsWhenMCPServersIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-no-mcp",
			UserID: "user-no-mcp",
			Scope:  "user",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// No config.toml should be created when neither ConfigTOML nor MCPServers is set.
	assert.NoFileExists(t, filepath.Join(outputDir, ".codex/config.toml"))
}
