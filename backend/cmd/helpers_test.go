package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpersCmd(t *testing.T) {
	assert.Equal(t, "helpers", HelpersCmd.Use)
	assert.Equal(t, "Helper utilities for agentapi-proxy", HelpersCmd.Short)
	assert.NotNil(t, HelpersCmd.Run)
}

func TestHelpersInit(t *testing.T) {
	// Test that subcommands are properly registered
	subcommands := HelpersCmd.Commands()

	var commandNames []string
	for _, cmd := range subcommands {
		commandNames = append(commandNames, cmd.Use)
	}

	assert.Contains(t, commandNames, "setup-claude-code")
	assert.Contains(t, commandNames, "setup-gh")
	assert.Contains(t, commandNames, "compile-settings")
}

func TestBuildStartupConfigCursor(t *testing.T) {
	config := buildStartupConfig("cursor", nil)

	assert.Equal(t, []string{"ccplant"}, config.Command)
	assert.Equal(t, []string{"acp-server", "--auto-approve", "--raw-json-log", "--", "agent", "acp"}, config.Args)
}

func TestBuildStartupConfigPiOllama(t *testing.T) {
	config := buildStartupConfig("pi-ollama", nil)

	assert.Equal(t, []string{"ccplant"}, config.Command)
	assert.Equal(t, []string{"acp-server", "--", "pi-acp"}, config.Args)
	assert.NotEmpty(t, config.PreScript)
	assert.Contains(t, config.PreScript, "node_modules/pi-ollama-cloud")
	assert.Contains(t, config.PreScript, "node_modules/pi-mcp-adapter")
	assert.Contains(t, config.PreScript, "skipping install")
}

func TestBuildStartupConfigUsesConfiguredProxyBinary(t *testing.T) {
	config := buildStartupConfig("codex-acp", map[string]string{
		"CCPLANT_BINARY_PATH": "/opt/ccplant/bin/ccplant",
	})

	assert.Equal(t, []string{"/opt/ccplant/bin/ccplant"}, config.Command)
}

func TestSetupClaudeCodeCmdStructure(t *testing.T) {
	assert.Equal(t, "setup-claude-code", setupClaudeCodeCmd.Use)
	assert.Equal(t, "Setup Claude Code configuration", setupClaudeCodeCmd.Short)
	assert.NotNil(t, setupClaudeCodeCmd.Run)
}

func TestRunSetupClaudeCodeHomeDir(t *testing.T) {
	// Test that the function can get the home directory
	// We can't easily test the full function since it creates files,
	// but we can verify the function exists and has the right structure
	assert.NotNil(t, setupClaudeCodeCmd.Run)
}

func TestHelpersRun(t *testing.T) {
	// Test the main helpers command run function doesn't panic
	assert.NotPanics(t, func() {
		HelpersCmd.Run(&cobra.Command{}, []string{})
	})
}

func TestSetupGHCmdStructure(t *testing.T) {
	assert.Equal(t, "setup-gh", setupGHCmd.Use)
	assert.Equal(t, "Setup GitHub authentication using gh CLI", setupGHCmd.Short)
	assert.NotNil(t, setupGHCmd.RunE)
}

func TestSetupGHFlags(t *testing.T) {
	// Test that all expected flags are present
	repoFlag := setupGHCmd.LocalFlags().Lookup("repo-fullname")
	assert.NotNil(t, repoFlag)

	appIDFlag := setupGHCmd.LocalFlags().Lookup("github-app-id")
	assert.NotNil(t, appIDFlag)

	installationIDFlag := setupGHCmd.LocalFlags().Lookup("github-installation-id")
	assert.NotNil(t, installationIDFlag)

	pemPathFlag := setupGHCmd.LocalFlags().Lookup("github-app-pem-path")
	assert.NotNil(t, pemPathFlag)

	pemFlag := setupGHCmd.LocalFlags().Lookup("github-app-pem")
	assert.NotNil(t, pemFlag)

	apiFlag := setupGHCmd.LocalFlags().Lookup("github-api")
	assert.NotNil(t, apiFlag)

	tokenFlag := setupGHCmd.LocalFlags().Lookup("github-token")
	assert.NotNil(t, tokenFlag)

	patFlag := setupGHCmd.LocalFlags().Lookup("github-personal-access-token")
	assert.NotNil(t, patFlag)

	stepFlag := setupGHCmd.LocalFlags().Lookup("step")
	assert.NotNil(t, stepFlag)
	assert.Equal(t, "all", stepFlag.DefValue)
}

func TestSetGitHubEnvFromFlags(t *testing.T) {
	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"GITHUB_APP_ID",
		"GITHUB_INSTALLATION_ID",
		"GITHUB_APP_PEM_PATH",
		"GITHUB_APP_PEM",
		"GITHUB_API",
		"GITHUB_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN",
		"GITHUB_REPO_FULLNAME",
	}

	for _, envVar := range envVars {
		originalEnv[envVar] = os.Getenv(envVar)
		_ = os.Unsetenv(envVar)
	}

	defer func() {
		// Restore original environment
		for _, envVar := range envVars {
			if val, ok := originalEnv[envVar]; ok && val != "" {
				_ = os.Setenv(envVar, val)
			} else {
				_ = os.Unsetenv(envVar)
			}
		}
	}()

	// Set flag variables
	githubAppID = "test-app-id"
	githubInstallationID = "test-installation-id"
	githubToken = "test-token"
	setupGHRepoFullName = "owner/repo"

	// Run the function
	err := setGitHubEnvFromFlags()
	assert.NoError(t, err)

	// Verify environment variables are set
	assert.Equal(t, "test-app-id", os.Getenv("GITHUB_APP_ID"))
	assert.Equal(t, "test-installation-id", os.Getenv("GITHUB_INSTALLATION_ID"))
	assert.Equal(t, "test-token", os.Getenv("GITHUB_TOKEN"))
	assert.Equal(t, "owner/repo", os.Getenv("GITHUB_REPO_FULLNAME"))

	// Clean up flag variables
	githubAppID = ""
	githubInstallationID = ""
	githubToken = ""
	setupGHRepoFullName = ""
}

func TestMergeMCPConfigCmdStructure(t *testing.T) {
	assert.Equal(t, "merge-mcp-config", mergeMCPConfigCmd.Use)
	assert.Equal(t, "Merge multiple MCP server configuration directories", mergeMCPConfigCmd.Short)
	assert.NotNil(t, mergeMCPConfigCmd.RunE)
}

func TestMergeMCPConfigCmdFlags(t *testing.T) {
	// Test that all expected flags are present
	inputDirsFlag := mergeMCPConfigCmd.LocalFlags().Lookup("input-dirs")
	assert.NotNil(t, inputDirsFlag)

	outputFlag := mergeMCPConfigCmd.LocalFlags().Lookup("output")
	assert.NotNil(t, outputFlag)

	expandEnvFlag := mergeMCPConfigCmd.LocalFlags().Lookup("expand-env")
	assert.NotNil(t, expandEnvFlag)
	assert.Equal(t, "false", expandEnvFlag.DefValue)

	verboseFlag := mergeMCPConfigCmd.LocalFlags().Lookup("verbose")
	assert.NotNil(t, verboseFlag)
	assert.Equal(t, "false", verboseFlag.DefValue)
}

func TestMergeMCPConfigCmdInSubcommands(t *testing.T) {
	subcommands := HelpersCmd.Commands()

	var commandNames []string
	for _, cmd := range subcommands {
		commandNames = append(commandNames, cmd.Use)
	}

	assert.Contains(t, commandNames, "merge-mcp-config")
}

func TestRunMergeMCPConfig(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "mcp-merge-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	inputDir := filepath.Join(tmpDir, "input")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create test config
	config := `{
		"mcpServers": {
			"test-server": {
				"type": "http",
				"url": "https://example.com/mcp"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(config), 0644))

	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	// Set flag values
	mcpInputDirs = inputDir
	mcpOutputPath = outputPath
	mcpExpandEnv = false
	mcpMergeVerbose = false

	// Run the command
	err = runMergeMCPConfig(&cobra.Command{}, []string{})
	require.NoError(t, err)

	// Verify output file was created
	assert.FileExists(t, outputPath)

	// Read and verify content
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))

	mcpServers, ok := result["mcpServers"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, mcpServers, "test-server")

	// Clean up flag variables
	mcpInputDirs = ""
	mcpOutputPath = ""
}

func TestRunMergeMCPConfigWithEnvExpansion(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "mcp-merge-env-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	inputDir := filepath.Join(tmpDir, "input")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	// Create test config with env var
	config := `{
		"mcpServers": {
			"api-server": {
				"type": "http",
				"url": "${TEST_MCP_URL:-https://default.example.com}"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(config), 0644))

	// Set env var
	require.NoError(t, os.Setenv("TEST_MCP_URL", "https://custom.example.com"))
	defer func() { _ = os.Unsetenv("TEST_MCP_URL") }()

	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	// Set flag values
	mcpInputDirs = inputDir
	mcpOutputPath = outputPath
	mcpExpandEnv = true
	mcpMergeVerbose = false

	// Run the command
	err = runMergeMCPConfig(&cobra.Command{}, []string{})
	require.NoError(t, err)

	// Read and verify content
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// Verify env var was expanded
	assert.Contains(t, string(data), "https://custom.example.com")
	assert.NotContains(t, string(data), "${TEST_MCP_URL")

	// Clean up flag variables
	mcpInputDirs = ""
	mcpOutputPath = ""
	mcpExpandEnv = false
}

func TestRunMergeMCPConfigMultipleDirs(t *testing.T) {
	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "mcp-merge-multi-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	baseDir := filepath.Join(tmpDir, "base")
	userDir := filepath.Join(tmpDir, "user")
	require.NoError(t, os.MkdirAll(baseDir, 0755))
	require.NoError(t, os.MkdirAll(userDir, 0755))

	// Base config
	baseConfig := `{
		"mcpServers": {
			"server": {"type": "http", "url": "https://base.example.com"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "config.json"), []byte(baseConfig), 0644))

	// User config (overrides server)
	userConfig := `{
		"mcpServers": {
			"server": {"type": "http", "url": "https://user.example.com"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "config.json"), []byte(userConfig), 0644))

	outputPath := filepath.Join(tmpDir, "merged.json")

	// Set flag values with comma-separated dirs
	mcpInputDirs = baseDir + "," + userDir
	mcpOutputPath = outputPath
	mcpExpandEnv = false
	mcpMergeVerbose = false

	// Run the command
	err = runMergeMCPConfig(&cobra.Command{}, []string{})
	require.NoError(t, err)

	// Read and verify content - user should override base
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	assert.Contains(t, string(data), "https://user.example.com")
	assert.NotContains(t, string(data), "https://base.example.com")

	// Clean up flag variables
	mcpInputDirs = ""
	mcpOutputPath = ""
}
