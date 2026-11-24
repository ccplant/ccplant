package sessionsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_FullSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-full-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create input settings
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-123",
			UserID:    "user-456",
			Scope:     "user",
			AgentType: "claude-acp",
		},
		Env: map[string]string{
			"AGENTAPI_PORT":       "9000",
			"AGENTAPI_SESSION_ID": "test-123",
			"HOME":                "/home/agentapi",
		},
		Claude: ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"testKey": "testValue",
			},
			SettingsJSON: map[string]interface{}{
				"settings": map[string]interface{}{
					"mcp.enabled": true,
				},
			},
			MCPServers: map[string]interface{}{
				"test-server": map[string]interface{}{
					"type": "http",
					"url":  "https://example.com",
				},
			},
		},
		Startup: StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--port", "9000"},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	envFile := filepath.Join(tmpDir, "env")
	startupFile := filepath.Join(tmpDir, "startup.sh")

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: envFile,
		StartupPath: startupFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Verify all files were created
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))
	assert.NoFileExists(t, envFile)
	assert.FileExists(t, startupFile)

	// Verify mcpServers is written into claude.json
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)
	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)
	assert.Contains(t, claudeJSON, "mcpServers")
}

func TestCompile_MinimalSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-minimal-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Minimal settings - only required fields
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "min-123",
			UserID: "user-789",
			Scope:  "user",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	envFile := filepath.Join(tmpDir, "env")
	startupFile := filepath.Join(tmpDir, "startup.sh")

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: envFile,
		StartupPath: startupFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Claude files should still be created with defaults
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))

	// Environment state remains process-only, even when empty.
	assert.NoFileExists(t, envFile)

	// No mcpServers key when no servers configured
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)
	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)
	assert.NotContains(t, claudeJSON, "mcpServers")
}

func TestCompile_ClaudeJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-claude-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-claude",
			UserID: "user-claude",
			Scope:  "user",
		},
		Claude: ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"customKey": "customValue",
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := tmpDir

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read and verify .claude.json
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)

	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)

	// Verify onboarding flags are always present
	assert.Equal(t, true, claudeJSON["hasCompletedOnboarding"])
	assert.Equal(t, true, claudeJSON["bypassPermissionsModeAccepted"])

	// Verify custom key is preserved
	assert.Equal(t, "customValue", claudeJSON["customKey"])
}

func TestCompile_DoesNotWriteEnvFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-env-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-env",
			UserID: "user-env",
			Scope:  "user",
		},
		Env: map[string]string{
			"Z_VAR":        "last",
			"A_VAR":        "first",
			"M_VAR":        "middle",
			"SPACED_VALUE": "value with spaces",
			"QUOTED_VALUE": `value with "quotes"`,
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	envFile := filepath.Join(tmpDir, "env")

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: envFile,
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	_, err = os.Stat(envFile)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestCompile_StartupScript(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-startup-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-startup",
			UserID: "user-startup",
			Scope:  "user",
		},
		Startup: StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--port", "9000", "--verbose"},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	startupFile := filepath.Join(tmpDir, "startup.sh")

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: startupFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read startup script
	data, err := os.ReadFile(startupFile)
	require.NoError(t, err)

	content := string(data)

	// Verify shebang
	assert.True(t, strings.HasPrefix(content, "#!/bin/sh"))

	// Verify command is present
	assert.Contains(t, content, "agentapi server")
	assert.Contains(t, content, "--port 9000")
	assert.Contains(t, content, "--verbose")

	// Verify file is executable
	info, err := os.Stat(startupFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestCompile_MCPServersInClaudeJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-mcp-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-mcp",
			UserID: "user-mcp",
			Scope:  "user",
		},
		Claude: ClaudeConfig{
			MCPServers: map[string]interface{}{
				"server1": map[string]interface{}{
					"type": "http",
					"url":  "https://server1.com",
				},
				"server2": map[string]interface{}{
					"type": "http",
					"url":  "https://server2.com",
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read and verify .claude.json contains mcpServers
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)

	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)

	// Verify mcpServers is embedded in claude.json
	assert.Contains(t, claudeJSON, "mcpServers")
	servers := claudeJSON["mcpServers"].(map[string]interface{})
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")

	// Verify onboarding flags are still present
	assert.Equal(t, true, claudeJSON["hasCompletedOnboarding"])
	assert.Equal(t, true, claudeJSON["bypassPermissionsModeAccepted"])
}

func TestCompile_AutoUpdatesChannelStable(t *testing.T) {
	t.Run("default settings has autoUpdatesChannel stable", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-autoupdates-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Minimal settings with no settingsJSON configured
		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-autoupdate",
				UserID: "user-autoupdate",
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

		// Read and verify settings.json
		settingsPath := filepath.Join(outputDir, ".claude/settings.json")
		data, err := os.ReadFile(settingsPath)
		require.NoError(t, err)

		var settingsJSON map[string]interface{}
		err = json.Unmarshal(data, &settingsJSON)
		require.NoError(t, err)

		assert.Equal(t, "stable", settingsJSON["autoUpdatesChannel"])
	})

	t.Run("custom settingsJSON also has autoUpdatesChannel stable", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-autoupdates-custom-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Settings with custom settingsJSON
		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-autoupdate-custom",
				UserID: "user-autoupdate-custom",
				Scope:  "user",
			},
			Claude: ClaudeConfig{
				SettingsJSON: map[string]interface{}{
					"settings": map[string]interface{}{
						"mcp.enabled": true,
					},
					"customKey": "customValue",
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

		// Read and verify settings.json
		settingsPath := filepath.Join(outputDir, ".claude/settings.json")
		data, err := os.ReadFile(settingsPath)
		require.NoError(t, err)

		var settingsJSON map[string]interface{}
		err = json.Unmarshal(data, &settingsJSON)
		require.NoError(t, err)

		// autoUpdatesChannel should always be "stable"
		assert.Equal(t, "stable", settingsJSON["autoUpdatesChannel"])
		// Custom key should be preserved
		assert.Equal(t, "customValue", settingsJSON["customKey"])
	})
}
