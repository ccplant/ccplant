package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

func parseInstallationID(installationIDStr string) (int64, error) {
	if installationIDStr == "" {
		return 0, fmt.Errorf("GITHUB_INSTALLATION_ID is required")
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("GITHUB_INSTALLATION_ID must be a valid integer: %w", err)
	}
	return installationID, nil
}

// findInstallationIDForRepoWithCache uses the installation cache to find installation ID
func findInstallationIDForRepoWithCache(appID int64, pemPath, repoFullName, apiBase string) (int64, error) {
	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			pemData = []byte(pemContent)
		} else {
			return 0, fmt.Errorf("failed to read PEM file %s and GITHUB_APP_PEM environment variable not set: %w", pemPath, err)
		}
	}

	ctx := context.Background()
	return installationCache.GetInstallationID(ctx, appID, pemData, repoFullName, apiBase)
}

// Legacy function kept for backward compatibility
func findInstallationIDForRepo(appID int64, pemPath, repoFullName, apiBase string) (int64, error) {
	return findInstallationIDForRepoWithCache(appID, pemPath, repoFullName, apiBase)
}

func authenticateGHCLI(githubURL, token string) error {
	fmt.Printf("Authenticating gh CLI for Enterprise Server: %s\n", githubURL)

	// Use gh auth login with token for Enterprise Server
	cmd := exec.Command("gh", "auth", "login", "--hostname", extractHostname(githubURL), "--with-token")
	cmd.Stdin = strings.NewReader(token)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth login failed: %w, stderr: %s", err, stderr.String())
	}

	fmt.Printf("Successfully authenticated gh CLI for %s\n", githubURL)
	return nil
}

func extractHostname(githubURL string) string {
	return github_pkg.ExtractHostname(githubURL)
}

func init() {
	initGitHubRepoCmd.Flags().BoolVar(&ignoreMissingConfig, "ignore-missing-config", false, "Skip execution when required configuration is not provided")
	initGitHubRepoCmd.Flags().StringVar(&repoFullName, "repo-fullname", "", "GitHub repository in org/repo format")
	initGitHubRepoCmd.Flags().StringVar(&cloneDir, "clone-dir", "", "Target directory for cloning")
	HelpersCmd.AddCommand(initGitHubRepoCmd)
}
