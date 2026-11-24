package startup

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

// Global installation cache instance for startup package
var installationCache = github_pkg.NewInstallationCache()

// SetupClaudeCode sets up Claude Code configuration
func SetupClaudeCode(homeDir string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", claudeConfigDir, err)
	}

	// Build Claude Code settings as a map
	settings := map[string]interface{}{
		"workspaceFolders": []interface{}{},
		"recentWorkspaces": []interface{}{},
		"settings": map[string]interface{}{
			"mcp.enabled": true,
		},
	}

	// Try to get GitHub token and add to env if available
	if githubToken, err := GetGitHubToken(""); err == nil && githubToken != "" {
		settings["env"] = map[string]string{
			"GITHUB_TOKEN": githubToken,
		}
		log.Printf("Added GITHUB_TOKEN to Claude Code settings")
	}

	// Marshal settings to JSON
	claudeCodeSettingsBytes, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings JSON: %w", err)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, claudeCodeSettingsBytes, 0644); err != nil {
		return fmt.Errorf("failed to write settings file %s: %w", settingsPath, err)
	}

	// Merge config into ~/.claude.json
	if err := mergeClaudeConfig(homeDir); err != nil {
		log.Printf("Warning: Failed to merge claude config: %v", err)
		// Don't return error, just warn
	}

	claudeSettingsMap := map[string]string{
		"hasTrustDialogAccepted":        "true",
		"hasCompletedProjectOnboarding": "true",
		"dontCrawlDirectory":            "true",
	}

	for key, value := range claudeSettingsMap {
		claudeCmd := exec.Command("claude", "config", "set", key, value)
		if err := claudeCmd.Run(); err != nil {
			log.Printf("Warning: Failed to set Claude config for key '%s': %v", key, err)
			// Don't return error for claude config, just warn
		}
	}

	log.Printf("Successfully created Claude Code configuration at %s", settingsPath)
	return nil
}

func mergeClaudeConfig(homeDir string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	targetPath := filepath.Join(homeDir, ".claude.json")

	// Define the configuration to merge as a map
	configToMerge := map[string]interface{}{
		"hasCompletedOnboarding":        true,
		"bypassPermissionsModeAccepted": true,
	}

	// Read existing ~/.claude.json if it exists
	var targetJSON map[string]interface{}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read ~/.claude.json: %w", err)
		}
		// File doesn't exist, we'll create it
		targetJSON = make(map[string]interface{})
	} else {
		// Parse existing JSON
		if err := json.Unmarshal(targetData, &targetJSON); err != nil {
			return fmt.Errorf("failed to parse ~/.claude.json: %w", err)
		}
	}

	// Merge configuration map into target
	for key, value := range configToMerge {
		targetJSON[key] = value
	}

	// Write merged JSON back
	mergedData, err := json.MarshalIndent(targetJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged JSON: %w", err)
	}

	if err := os.WriteFile(targetPath, mergedData, 0644); err != nil {
		return fmt.Errorf("failed to write merged ~/.claude.json: %w", err)
	}

	return nil
}

// MCPServerConfig represents MCP server configuration
type MCPServerConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Endpoint  string            `json:"endpoint"`
	Enabled   bool              `json:"enabled"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
}

// SetupMCPServers sets up MCP servers from configuration
func SetupMCPServers(homeDir, configFlag string) error {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}
	claudeDir := filepath.Join(homeDir, ".claude")

	if configFlag == "" {
		return fmt.Errorf("config flag is required")
	}

	// Decode base64 configuration
	configJson, err := decodeBase64(configFlag)
	if err != nil {
		return fmt.Errorf("failed to decode base64 config: %w", err)
	}

	// Parse MCP server configurations
	var mcpConfigs []MCPServerConfig
	if err := json.Unmarshal([]byte(configJson), &mcpConfigs); err != nil {
		return fmt.Errorf("failed to parse MCP configuration: %w", err)
	}

	log.Printf("Adding %d MCP servers to Claude configuration", len(mcpConfigs))

	// Add each MCP server using claude command
	for _, mcpConfig := range mcpConfigs {
		if err := addMcpServer(claudeDir, mcpConfig); err != nil {
			log.Printf("Warning: Failed to add MCP server %s: %v", mcpConfig.Name, err)
			// Continue with other servers even if one fails
		} else {
			log.Printf("Successfully added MCP server: %s", mcpConfig.Name)
		}
	}

	return nil
}

func decodeBase64(encoded string) (string, error) {
	// Try base64 decoding first
	if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return string(decoded), nil
	}

	// If base64 fails, try returning as-is (assume it's already decoded)
	return encoded, nil
}

func addMcpServer(claudeDir string, mcpConfig MCPServerConfig) error {
	// Build claude mcp add command
	args := []string{"mcp", "add", mcpConfig.Name}

	// Add command and args for stdio transport
	if mcpConfig.Transport == "stdio" && mcpConfig.Command != "" {
		args = append(args, mcpConfig.Command)
		if len(mcpConfig.Args) > 0 {
			args = append(args, mcpConfig.Args...)
		}
	}

	// Set environment variables
	env := os.Environ()

	// Add custom environment variables from MCP config
	for key, value := range mcpConfig.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Execute claude command
	claudeCmd := exec.Command("claude", args...)
	claudeCmd.Env = env
	claudeCmd.Dir = claudeDir

	output, err := claudeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude command failed: %w, output: %s", err, string(output))
	}

	log.Printf("Claude mcp add output for %s: %s", mcpConfig.Name, string(output))
	return nil
}

// InitGitHubRepo initializes a GitHub repository with proper git clone functionality
func InitGitHubRepo(repoFullName, cloneDir string, ignoreMissingConfig bool) error {
	log.Printf("Initializing GitHub repository: %s to %s", repoFullName, cloneDir)

	if repoFullName == "" {
		if ignoreMissingConfig {
			log.Printf("Warning: No repository fullname provided, skipping GitHub initialization")
			return nil
		}
		return fmt.Errorf("repository fullname is required")
	}

	// Create the clone directory
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		return fmt.Errorf("failed to create clone directory: %w", err)
	}

	// Get GitHub token for authentication
	token, err := GetGitHubToken(repoFullName)
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Create repository URL using the correct GitHub URL
	githubURL := getGitHubURL()
	repoURL := fmt.Sprintf("%s/%s", githubURL, repoFullName)

	// Setup the repository with proper git clone
	if err := setupRepository(repoURL, token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	log.Printf("GitHub repository initialization completed successfully")
	return nil
}

// GetGitHubToken retrieves the GitHub token for authentication
func GetGitHubToken(repoFullName string) (string, error) {
	// Check for personal access token first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}

	// If no token available, try to use GitHub App authentication
	appID := os.Getenv("GITHUB_APP_ID")
	installationID := os.Getenv("GITHUB_INSTALLATION_ID")
	pemPath := os.Getenv("GITHUB_APP_PEM_PATH")

	// GitHub App authentication requires App ID and PEM path
	if appID != "" && pemPath != "" {
		// If Installation ID is not provided, try auto-discovery
		if installationID == "" {
			// Auto-discovery requires repository fullname
			if repoFullName == "" {
				// Fall back to environment variable if not provided
				repoFullName = os.Getenv("GITHUB_REPO_FULLNAME")
			}
			if repoFullName == "" {
				log.Println("GITHUB_INSTALLATION_ID not provided and repoFullName not available for auto-discovery")
				// Fall through to next authentication method
			} else {
				log.Printf("GITHUB_INSTALLATION_ID not provided, attempting auto-discovery for repository: %s", repoFullName)
				discoveredID, err := AutoDiscoverInstallationID(appID, pemPath, repoFullName)
				if err != nil {
					log.Printf("Failed to auto-discover installation ID: %v", err)
					// Fall through to next authentication method
				} else {
					installationID = discoveredID
					log.Printf("Auto-discovered installation ID: %s", installationID)
				}
			}
		}

		// If we have installation ID (manual or auto-discovered), proceed with token generation
		if installationID != "" {
			return GenerateGitHubAppTokenForRepository(appID, installationID, pemPath, repoFullName)
		}
	}

	// Check for GITHUB_PERSONAL_ACCESS_TOKEN as fallback
	if token := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); token != "" {
		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication found: GITHUB_TOKEN, GITHUB_PERSONAL_ACCESS_TOKEN, or GitHub App credentials (GITHUB_APP_ID, GITHUB_APP_PEM_PATH) are required. GITHUB_INSTALLATION_ID is optional and will be auto-discovered if GITHUB_REPO_FULLNAME is set")
}

// GenerateGitHubAppToken generates a GitHub App installation token
func GenerateGitHubAppToken(appIDStr, installationIDStr, pemPath string) (string, error) {
	return GenerateGitHubAppTokenForRepository(appIDStr, installationIDStr, pemPath, "")
}

// GenerateGitHubAppTokenForRepository generates a GitHub App installation token,
// optionally restricted to a single repository.
func GenerateGitHubAppTokenForRepository(appIDStr, installationIDStr, pemPath, repoFullName string) (string, error) {
	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			log.Printf("Failed to read PEM file %s, using GITHUB_APP_PEM environment variable", pemPath)
			pemData = []byte(pemContent)
		} else {
			// より詳細なエラー情報を提供
			fileInfo, statErr := os.Stat(pemPath)
			if statErr != nil {
				return "", fmt.Errorf("failed to read PEM file %s: file does not exist or is not accessible. Also checked GITHUB_APP_PEM environment variable: %w", pemPath, err)
			}
			return "", fmt.Errorf("failed to read PEM file %s (size: %d bytes, mode: %s). Also checked GITHUB_APP_PEM environment variable: %w",
				pemPath, fileInfo.Size(), fileInfo.Mode(), err)
		}
	}

	return GenerateGitHubAppTokenFromPEM(appIDStr, installationIDStr, pemData, repoFullName)
}

// GenerateGitHubAppTokenFromPEM generates an installation token from in-memory
// GitHub App credentials. This is used when credentials are loaded from a
// Kubernetes Secret while constructing initial session settings.
