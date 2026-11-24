package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	"github.com/spf13/cobra"
	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

type GitHubAppConfig struct {
	AppID          int64
	InstallationID int64
	PEMPath        string
	APIBase        string
}

// Global installation cache instance
var installationCache = github_pkg.NewInstallationCache()

var initGitHubRepoCmd = &cobra.Command{
	Use:   "init-github-repository",
	Short: "Initialize GitHub repository with authentication",
	Long: `Initialize GitHub repository with proper authentication.
This command will:
1. Validate GitHub authentication (token or app credentials)
2. Clone or update the specified GitHub repository
3. Set up MCP integration with Claude

Parameters:
- --repo-fullname: The GitHub repository in org/repo format (e.g., "owner/repository")
- --clone-dir: Target directory for cloning (defaults to current directory)

Authentication options (one required):
- GITHUB_TOKEN: Personal access token or existing token
- GitHub App credentials:
  - GITHUB_APP_PEM_PATH: Path to private key file (or use GITHUB_APP_PEM environment variable)
  - GITHUB_APP_ID: GitHub App ID
  - GITHUB_INSTALLATION_ID: Installation ID (optional, will auto-detect from repository if not provided)

Optional:
- GITHUB_API: GitHub API base URL (defaults to https://api.github.com)
- GITHUB_URL: GitHub base URL (defaults to https://github.com)
`,
	RunE: runInitGitHubRepo,
}

var ignoreMissingConfig bool
var repoFullName string
var cloneDir string

// RunInitGitHubRepo is exported for use in other packages
func RunInitGitHubRepo(repoFullName, cloneDir string, ignoreMissingConfig bool) error {
	return initGitHubRepoInternal(repoFullName, cloneDir, ignoreMissingConfig)
}

func runInitGitHubRepo(cmd *cobra.Command, args []string) error {
	repoFullName, err := cmd.Flags().GetString("repo-fullname")
	if err != nil {
		return fmt.Errorf("read repo-fullname flag: %w", err)
	}
	cloneDir, err := cmd.Flags().GetString("clone-dir")
	if err != nil {
		return fmt.Errorf("read clone-dir flag: %w", err)
	}
	ignoreMissingConfig, err := cmd.Flags().GetBool("ignore-missing-config")
	if err != nil {
		return fmt.Errorf("read ignore-missing-config flag: %w", err)
	}

	return initGitHubRepoInternal(repoFullName, cloneDir, ignoreMissingConfig)
}

func initGitHubRepoInternal(repoFullNameParam, cloneDirParam string, ignoreMissingConfigParam bool) error {
	repoFullName := repoFullNameParam
	cloneDir := cloneDirParam
	ignoreMissingConfig := ignoreMissingConfigParam
	// Get repository fullname from flag or environment variable
	if repoFullName == "" {
		repoFullName = os.Getenv("GITHUB_REPO_FULLNAME")
	}
	if repoFullName == "" && !ignoreMissingConfig {
		return fmt.Errorf("repository fullname is required (use --repo-fullname flag or GITHUB_REPO_FULLNAME environment variable)")
	}

	token, err := resolveGitHubInitToken(repoFullName, ignoreMissingConfig)
	if err != nil {
		return err
	}
	if token == "" {
		return nil
	}

	// Authenticate gh CLI for Enterprise Server if GITHUB_URL is set
	githubURL := getGitHubURL()
	if githubURL != "https://github.com" {
		if err := authenticateGHCLI(githubURL, token); err != nil {
			return fmt.Errorf("failed to authenticate gh CLI for Enterprise Server: %w", err)
		}
	}

	// Get clone directory from flag or environment variable
	cloneDir, err = resolveGitHubCloneDir(cloneDir)
	if err != nil {
		return err
	}

	// Convert fullname to URL for cloning
	repoURL := fmt.Sprintf("%s/%s", githubURL, repoFullName)

	// Setup repository
	if err := setupRepository(repoURL, token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup repository: %w", err)
	}

	// Setup MCP integration
	if err := setupMCPIntegration(token, cloneDir); err != nil {
		return fmt.Errorf("failed to setup MCP integration: %w", err)
	}

	// Save environment variables for future use
	if err := saveEnvironmentVariables(token, cloneDir); err != nil {
		fmt.Printf("Warning: failed to save environment variables: %v\n", err)
	}

	fmt.Println("GitHub repository initialization completed successfully!")
	return nil
}

func resolveGitHubInitToken(repoFullName string, ignoreMissingConfig bool) (string, error) {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}
	if ignoreMissingConfig {
		fmt.Println("Skipping GitHub setup due to --ignore-missing-config flag")
		return "", nil
	}
	fmt.Println("No GITHUB_TOKEN found, attempting to generate from GitHub App credentials...")
	appID, err := parseAppID(os.Getenv("GITHUB_APP_ID"))
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}
	installationID, err := resolveGitHubInstallationID(appID, repoFullName)
	if err != nil {
		return "", err
	}
	config := GitHubAppConfig{AppID: appID, InstallationID: installationID, PEMPath: os.Getenv("GITHUB_APP_PEM_PATH"), APIBase: getAPIBase()}
	if config.AppID == 0 || config.InstallationID == 0 || config.PEMPath == "" {
		return "", fmt.Errorf("either GITHUB_TOKEN or GitHub App credentials (GITHUB_APP_ID, GITHUB_APP_PEM_PATH) must be provided")
	}
	token, err := generateInstallationToken(config)
	if err != nil {
		return "", fmt.Errorf("failed to generate installation token: %w", err)
	}
	fmt.Println("Successfully generated installation token")
	return token, nil
}

func resolveGitHubInstallationID(appID int64, repoFullName string) (int64, error) {
	if value := os.Getenv("GITHUB_INSTALLATION_ID"); value != "" {
		installationID, err := parseInstallationID(value)
		if err != nil {
			return 0, fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
		}
		return installationID, nil
	}
	fmt.Println("No GITHUB_INSTALLATION_ID provided, attempting auto-detection with cache...")
	installationID, err := findInstallationIDForRepoWithCache(appID, os.Getenv("GITHUB_APP_PEM_PATH"), repoFullName, getAPIBase())
	if err != nil {
		return 0, fmt.Errorf("failed to auto-detect installation ID: %w", err)
	}
	fmt.Printf("Auto-detected installation ID: %d\n", installationID)
	return installationID, nil
}

func resolveGitHubCloneDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if cloneDir := os.Getenv("GITHUB_CLONE_DIR"); cloneDir != "" {
		return cloneDir, nil
	}
	cloneDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return cloneDir, nil
}

func getAPIBase() string {
	return github_pkg.GetAPIBase()
}

func getGitHubURL() string {
	return github_pkg.GetGitHubURL()
}

func generateInstallationToken(config GitHubAppConfig) (string, error) {
	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err := os.ReadFile(config.PEMPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			fmt.Printf("Failed to read PEM file %s, using GITHUB_APP_PEM environment variable\n", config.PEMPath)
			pemData = []byte(pemContent)
		} else {
			// より詳細なエラー情報を提供
			fileInfo, statErr := os.Stat(config.PEMPath)
			if statErr != nil {
				return "", fmt.Errorf("failed to read PEM file %s: file does not exist or is not accessible. Also checked GITHUB_APP_PEM environment variable: %w", config.PEMPath, err)
			}
			return "", fmt.Errorf("failed to read PEM file %s (size: %d bytes, mode: %s). Also checked GITHUB_APP_PEM environment variable: %w",
				config.PEMPath, fileInfo.Size(), fileInfo.Mode(), err)
		}
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, config.AppID, pemData)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if config.APIBase != "" && config.APIBase != "https://api.github.com" {
		transport.BaseURL = config.APIBase
	}

	// Create GitHub client
	var client *github.Client
	if config.APIBase == "" || strings.Contains(config.APIBase, "https://api.github.com") {
		client = github.NewClient(&http.Client{Transport: transport})
	} else {
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(config.APIBase, config.APIBase)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	// Create installation token
	ctx := context.Background()
	token, response, err := client.Apps.CreateInstallationToken(
		ctx,
		config.InstallationID,
		&github.InstallationTokenOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}
	if token == nil || response == nil {
		return "", fmt.Errorf("GitHub installation token response was incomplete")
	}

	return token.GetToken(), nil
}

func setupRepository(repoURL, token, cloneDir string) error {
	fmt.Printf("Setting up repository in: %s\n", cloneDir)

	// Check if directory exists and is a git repository
	gitDir := filepath.Join(cloneDir, ".git")
	isGitRepo := false
	if _, err := os.Stat(gitDir); err == nil {
		isGitRepo = true
	}

	// Create authenticated URL
	authURL, err := github_pkg.CreateAuthenticatedURL(repoURL, token)
	if err != nil {
		return fmt.Errorf("failed to create authenticated URL: %w", err)
	}

	if isGitRepo {
		return updateGitHubRepository(cloneDir)
	}
	if err := initializeGitHubRepository(cloneDir, authURL); err != nil {
		return err
	}
	fmt.Println("Repository setup completed")
	return nil
}

func updateGitHubRepository(cloneDir string) error {
	fmt.Println("Git repository detected, updating...")
	cmd := exec.Command("git", "pull")
	cmd.Dir = cloneDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull updates: %w", err)
	}
	fmt.Println("Repository setup completed")
	return nil
}

func initializeGitHubRepository(cloneDir, authURL string) error {
	fmt.Println("Initializing new git repository...")
	if err := os.MkdirAll(cloneDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	commands := []struct {
		args  []string
		label string
	}{{[]string{"init"}, "initialize git"}, {[]string{"remote", "add", "origin", authURL}, "add remote"}, {[]string{"fetch"}, "fetch"}}
	for _, command := range commands {
		cmd := exec.Command("git", command.args...)
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to %s: %w", command.label, err)
		}
	}
	var checkoutErr error
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "checkout", branch)
		cmd.Dir = cloneDir
		if checkoutErr = cmd.Run(); checkoutErr == nil {
			return nil
		}
	}
	return fmt.Errorf("failed to checkout main or master branch: %w", checkoutErr)
}

func setupMCPIntegration(token, cloneDir string) error {
	fmt.Println("Setting up MCP integration...")

	// Add GitHub MCP server to Claude
	cmd := exec.Command("claude",
		"mcp", "add", "github",
		"--",
		"docker", "run", "-i", "--rm",
		"-e", "GITHUB_PERSONAL_ACCESS_TOKEN="+token,
		"ghcr.io/github/github-mcp-server")
	cmd.Dir = cloneDir
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: failed to add GitHub MCP server to Claude: %v\n", err)
	}
	fmt.Println("MCP integration setup completed")
	return nil
}

func saveEnvironmentVariables(token, cloneDir string) error {
	pid := os.Getpid()
	envFile := fmt.Sprintf("/tmp/github_env_%d", pid)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "GITHUB_TOKEN=%s\n", token)
	fmt.Fprintf(&buf, "GITHUB_CLONE_DIR=%s\n", cloneDir)

	return os.WriteFile(envFile, buf.Bytes(), 0600)
}

func parseAppID(appIDStr string) (int64, error) {
	if appIDStr == "" {
		return 0, fmt.Errorf("GITHUB_APP_ID is required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("GITHUB_APP_ID must be a valid integer: %w", err)
	}
	return appID, nil
}
