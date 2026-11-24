package sessionsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_PiMCPConfig(t *testing.T) {
	t.Run("writes shared mcp config for pi", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-pi-mcp-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-pi-mcp",
				UserID:    "user-pi-mcp",
				Scope:     "user",
				AgentType: "pi",
			},
			Claude: ClaudeConfig{
				MCPServers: map[string]interface{}{
					"github": map[string]interface{}{
						"type":    "stdio",
						"command": "github-mcp-server",
						"args":    []interface{}{"stdio"},
						"env": map[string]interface{}{
							"GITHUB_TOKEN": "ghp_secret",
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

		configPath := filepath.Join(outputDir, ".config/mcp/mcp.json")
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)

		var config map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &config))
		mcpServers, ok := config["mcpServers"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, mcpServers, "github")
	})

	t.Run("skips non-pi sessions", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-non-pi-mcp-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-claude-mcp",
				UserID:    "user-claude-mcp",
				Scope:     "user",
				AgentType: "claude-acp",
			},
			Claude: ClaudeConfig{
				MCPServers: map[string]interface{}{
					"github": map[string]interface{}{
						"type": "http",
						"url":  "https://mcp.example.com",
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

		assert.NoFileExists(t, filepath.Join(outputDir, ".config/mcp/mcp.json"))
	})
}

func TestGeneratePiSettingsJSON(t *testing.T) {
	t.Run("merges managed settings into existing file", func(t *testing.T) {
		outputDir := t.TempDir()
		piDir := filepath.Join(outputDir, ".pi", "agent")
		require.NoError(t, os.MkdirAll(piDir, 0755))
		settingsPath := filepath.Join(piDir, "settings.json")
		require.NoError(t, os.WriteFile(settingsPath, []byte(`{"theme":"dark","defaultModel":"old-model"}`), 0644))

		err := generatePiSettingsJSON(outputDir, map[string]interface{}{
			"defaultProvider":      "ollama-cloud",
			"defaultModel":         "qwen3-coder",
			"defaultThinkingLevel": "high",
		})
		require.NoError(t, err)

		data, err := os.ReadFile(settingsPath)
		require.NoError(t, err)
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, "dark", got["theme"])
		assert.Equal(t, "ollama-cloud", got["defaultProvider"])
		assert.Equal(t, "qwen3-coder", got["defaultModel"])
		assert.Equal(t, "high", got["defaultThinkingLevel"])

		info, err := os.Stat(settingsPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("writes settings regardless of agent type", func(t *testing.T) {
		outputDir := t.TempDir()
		err := generatePiSettingsJSON(outputDir, map[string]interface{}{
			"defaultModel": "qwen3-coder",
		})
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(outputDir, ".pi", "agent", "settings.json"))
		require.NoError(t, err)
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, "qwen3-coder", got["defaultModel"])
	})

	t.Run("skips empty settings", func(t *testing.T) {
		outputDir := t.TempDir()
		require.NoError(t, generatePiSettingsJSON(outputDir, nil))
		assert.NoFileExists(t, filepath.Join(outputDir, ".pi", "agent", "settings.json"))
	})

	t.Run("rejects invalid existing settings", func(t *testing.T) {
		outputDir := t.TempDir()
		piDir := filepath.Join(outputDir, ".pi", "agent")
		require.NoError(t, os.MkdirAll(piDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(piDir, "settings.json"), []byte(`not-json`), 0600))

		err := generatePiSettingsJSON(outputDir, map[string]interface{}{
			"defaultModel": "qwen3-coder",
		})
		require.ErrorContains(t, err, "failed to parse existing Pi settings.json")
	})
}

func TestGeneratePiModelsJSON(t *testing.T) {
	outputDir := t.TempDir()
	piDir := filepath.Join(outputDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piDir, 0755))
	modelsPath := filepath.Join(piDir, "models.json")
	require.NoError(t, os.WriteFile(modelsPath, []byte(`{
		"providers": {
			"ollama-cloud": {
				"headers": {"x-existing": "preserved"},
				"models": [{"id": "existing-model"}]
			},
			"other": {"baseUrl": "https://other.example/v1"}
		}
	}`), 0644))

	err := generatePiModelsJSON(outputDir, map[string]interface{}{
		"providers": map[string]interface{}{
			"ollama-cloud": map[string]interface{}{
				"baseUrl": "https://ollama.com/v1",
				"api":     "openai-completions",
				"apiKey":  "$OLLAMA_API_KEY",
				"models": []interface{}{
					map[string]interface{}{"id": "glm-5.2:cloud", "reasoning": true},
				},
			},
		},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(modelsPath)
	require.NoError(t, err)
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &got))
	providers := got["providers"].(map[string]interface{})
	assert.Contains(t, providers, "other")
	provider := providers["ollama-cloud"].(map[string]interface{})
	assert.Contains(t, provider, "headers")
	assert.Equal(t, "$OLLAMA_API_KEY", provider["apiKey"])
	models := provider["models"].([]interface{})
	require.Len(t, models, 2)
	assert.Equal(t, "existing-model", models[0].(map[string]interface{})["id"])
	assert.Equal(t, "glm-5.2:cloud", models[1].(map[string]interface{})["id"])

	info, err := os.Stat(modelsPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestGeneratePiModelsJSONSkipsEmptyConfig(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, generatePiModelsJSON(outputDir, nil))
	assert.NoFileExists(t, filepath.Join(outputDir, ".pi", "agent", "models.json"))
}

func TestGenerateCodexInstructionsUsesDiscoveredGlobalPath(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, generateCodexInstructionsMD(outputDir, "follow these instructions"))

	agentsPath := filepath.Join(outputDir, ".codex", "AGENTS.md")
	assert.FileExists(t, agentsPath)
	assert.NoFileExists(t, filepath.Join(outputDir, ".codex", "instructions.md"))
	content, err := os.ReadFile(agentsPath)
	require.NoError(t, err)
	assert.Equal(t, "follow these instructions", string(content))
}

func TestCompile_MissingInput(t *testing.T) {
	opts := CompileOptions{
		InputPath:   "/nonexistent/settings.yaml",
		OutputDir:   "/tmp",
		EnvFilePath: "/tmp/env",
		StartupPath: "/tmp/startup.sh",
	}

	err := Compile(opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read settings file")
}

func TestDefaultCompileOptions(t *testing.T) {
	opts := DefaultCompileOptions()

	assert.Equal(t, "/session-settings/settings.yaml", opts.InputPath)
	assert.Equal(t, "/home/agentapi", opts.OutputDir)
	assert.Equal(t, "/home/agentapi/.session/env", opts.EnvFilePath)
	assert.Equal(t, "/home/agentapi/.session/startup.sh", opts.StartupPath)
}
