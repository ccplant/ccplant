package startup

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

func GenerateGitHubAppTokenFromPEM(appIDStr, installationIDStr string, pemData []byte, repoFullName string) (string, error) {
	repositories, err := installationTokenRepositories(repoFullName)
	if err != nil {
		return "", err
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_INSTALLATION_ID: %w", err)
	}

	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pemData)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		transport.BaseURL = githubAPI
	}

	// Create GitHub client
	client := github.NewClient(&http.Client{Transport: transport})

	// Handle GitHub Enterprise
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" {
		var err error
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(githubAPI, githubAPI)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	// Create installation token
	ctx := context.Background()
	token, _, err := client.Apps.CreateInstallationToken(
		ctx,
		installationID,
		&github.InstallationTokenOptions{
			Repositories: repositories,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to create installation token: %w", err)
	}

	return token.GetToken(), nil
}

func installationTokenRepositories(repoFullName string) ([]string, error) {
	repoFullName = strings.TrimSpace(repoFullName)
	if repoFullName == "" {
		if repositoryRestrictionEnabled() {
			return nil, fmt.Errorf("repository restriction is enabled; repository fullname is required for GitHub App installation token generation")
		}
		return nil, nil
	}

	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("invalid repository fullname %q: expected owner/repo", repoFullName)
	}

	return []string{strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git")}, nil
}

func repositoryRestrictionEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("REPOSITORY_RESTRICTION")), "true")
}

// AutoDiscoverInstallationID discovers the installation ID for a given repository using cache
func AutoDiscoverInstallationID(appIDStr, pemPath, repoFullName string) (string, error) {
	// Parse app ID
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}

	// Read private key - try file first, then fallback to environment variable
	var pemData []byte

	// Try to read from file first
	pemData, err = os.ReadFile(pemPath)
	if err != nil {
		// If file read fails, try to get from environment variable
		if pemContent := os.Getenv("GITHUB_APP_PEM"); pemContent != "" {
			pemData = []byte(pemContent)
		} else {
			return "", fmt.Errorf("failed to read PEM file %s and GITHUB_APP_PEM environment variable not set: %w", pemPath, err)
		}
	}

	// Get API base URL
	apiBase := os.Getenv("GITHUB_API")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}

	ctx := context.Background()
	installationID, err := installationCache.GetInstallationID(ctx, appID, pemData, repoFullName, apiBase)
	if err != nil {
		return "", err
	}

	return strconv.FormatInt(installationID, 10), nil
}

// GenerateMarketplaceGitHubAppToken discovers the built-in GitHub App
// installation for repositoryURL and returns a contents:read token restricted
// to that repository. It deliberately ignores GITHUB_TOKEN and
// GITHUB_INSTALLATION_ID, which belong to the session's working repository.
func GenerateMarketplaceGitHubAppToken(ctx context.Context, repositoryURL string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(github_pkg.ExtractRepositoryHostname(repositoryURL)))
	repoFullName := strings.TrimSpace(github_pkg.ParseRepositoryURL(repositoryURL))
	parts := strings.Split(repoFullName, "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid GitHub marketplace repository URL %q", repositoryURL)
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	if parts[1] == "" {
		return "", fmt.Errorf("invalid GitHub marketplace repository URL %q", repositoryURL)
	}

	apiBase, err := marketplaceGitHubAPIBase(host)
	if err != nil {
		return "", err
	}
	appIDString := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	if appIDString == "" {
		return "", fmt.Errorf("GITHUB_APP_ID is not configured")
	}
	appID, err := strconv.ParseInt(appIDString, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid GITHUB_APP_ID: %w", err)
	}
	pemData, err := readGitHubAppPEM(os.Getenv("GITHUB_APP_PEM_PATH"))
	if err != nil {
		return "", err
	}

	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pemData)
	if err != nil {
		return "", fmt.Errorf("create GitHub App transport: %w", err)
	}
	transport.BaseURL = apiBase
	client := github.NewClient(&http.Client{Transport: transport})
	if apiBase != "https://api.github.com" {
		apiURL := strings.TrimSuffix(apiBase, "/") + "/"
		client, err = client.WithEnterpriseURLs(apiURL, apiURL)
		if err != nil {
			return "", fmt.Errorf("configure GitHub Enterprise API: %w", err)
		}
	}

	installation, _, err := client.Apps.FindRepositoryInstallation(ctx, parts[0], parts[1])
	if err != nil {
		return "", fmt.Errorf("find GitHub App installation for %s: %w", repoFullName, err)
	}
	result, _, err := client.Apps.CreateInstallationToken(ctx, installation.GetID(), &github.InstallationTokenOptions{
		Repositories: []string{parts[1]},
		Permissions:  &github.InstallationPermissions{Contents: github.String("read")},
	})
	if err != nil {
		return "", fmt.Errorf("create GitHub App token for %s: %w", repoFullName, err)
	}
	if result.GetToken() == "" {
		return "", fmt.Errorf("GitHub returned an empty installation token for %s", repoFullName)
	}
	return result.GetToken(), nil
}

func marketplaceGitHubAPIBase(host string) (string, error) {
	if host == "github.com" {
		return "https://api.github.com", nil
	}
	configuredHost := strings.ToLower(github_pkg.ExtractHostname(github_pkg.GetGitHubURL()))
	if host != configuredHost {
		return "", fmt.Errorf("unsupported GitHub marketplace host %q", host)
	}
	return strings.TrimSuffix(github_pkg.GetAPIBase(), "/"), nil
}

func readGitHubAppPEM(pemPath string) ([]byte, error) {
	if strings.TrimSpace(pemPath) != "" {
		if pemData, err := os.ReadFile(pemPath); err == nil {
			return pemData, nil
		}
	}
	if pem := os.Getenv("GITHUB_APP_PEM"); pem != "" {
		return []byte(pem), nil
	}
	return nil, fmt.Errorf("GitHub App private key is not configured")
}

// setupRepository sets up the git repository using gh repo clone
func setupRepository(repoURL, token, cloneDir string) error {
	log.Printf("Setting up repository in: %s", cloneDir)

	// Check if directory exists and is a git repository
	gitDir := filepath.Join(cloneDir, ".git")
	isGitRepo := false
	if _, err := os.Stat(gitDir); err == nil {
		isGitRepo = true
	}

	if isGitRepo {
		log.Printf("Git repository detected, updating...")
		cmd := exec.Command("git", "pull")
		cmd.Dir = cloneDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to pull updates: %w", err)
		}
	} else {
		log.Printf("Cloning repository using gh repo clone...")

		// Extract repository name from URL
		repoName, err := extractRepoName(repoURL)
		if err != nil {
			return fmt.Errorf("failed to extract repository name: %w", err)
		}

		// Set up environment for gh command
		env := os.Environ()
		env = append(env, fmt.Sprintf("GITHUB_TOKEN=%s", token))

		// Set GitHub Enterprise host if applicable
		if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
			githubHost := strings.TrimPrefix(githubAPI, "https://")
			githubHost = strings.TrimPrefix(githubHost, "http://")
			githubHost = strings.TrimSuffix(githubHost, "/api/v3")
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))

			// Authenticate gh CLI for GitHub Enterprise Server
			if err := AuthenticateGHCLI(githubHost, token, env); err != nil {
				log.Printf("Warning: Failed to authenticate gh CLI for %s: %v", githubHost, err)
			}
		}

		// Create parent directory
		parentDir := filepath.Dir(cloneDir)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Use gh repo clone command
		cmd := exec.Command("gh", "repo", "clone", repoName, cloneDir)
		cmd.Env = env
		cmd.Dir = parentDir

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to clone repository with gh: %w, output: %s", err, string(output))
		}

		log.Printf("Successfully cloned repository using gh repo clone")
	}

	log.Printf("Repository setup completed")
	return nil
}

// AuthenticateGHCLI authenticates gh CLI for GitHub Enterprise Server
func AuthenticateGHCLI(githubHost, token string, env []string) error {
	log.Printf("Authenticating gh CLI for Enterprise Server: %s", githubHost)

	// Use gh auth login with token for Enterprise Server
	cmd := exec.Command("gh", "auth", "login", "--hostname", githubHost, "--with-token")
	cmd.Stdin = strings.NewReader(token)

	// Set environment variables to use user-specific home directory
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth login failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully authenticated gh CLI for %s", githubHost)
	return nil
}

// extractRepoName extracts owner/repo from various GitHub URL formats
func extractRepoName(repoURL string) (string, error) {
	// Handle various URL formats
	if strings.HasPrefix(repoURL, "git@") {
		// git@github.com:owner/repo.git or git@github.enterprise.com:owner/repo.git
		parts := strings.Split(repoURL, ":")
		if len(parts) >= 2 {
			repoPath := parts[len(parts)-1]
			return strings.TrimSuffix(repoPath, ".git"), nil
		}
	} else if strings.Contains(repoURL, "://") {
		// https://github.com/owner/repo or https://github.enterprise.com/owner/repo
		parts := strings.Split(repoURL, "/")
		if len(parts) >= 2 {
			owner := parts[len(parts)-2]
			repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
			return fmt.Sprintf("%s/%s", owner, repo), nil
		}
	}

	return "", fmt.Errorf("unable to extract repository name from URL: %s", repoURL)
}

// getGitHubURL returns the GitHub URL (supports enterprise)
func getGitHubURL() string {
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" {
		// For enterprise GitHub, convert API URL to web URL
		if strings.HasSuffix(githubAPI, "/api/v3") {
			return strings.TrimSuffix(githubAPI, "/api/v3")
		}
		return githubAPI
	}
	return "https://github.com"
}

// SetupGitHubAuth sets up GitHub authentication using gh CLI
func SetupGitHubAuth(repoFullName string) error {
	return SetupGitHubAuthStep(repoFullName, "all")
}

// SetupGitHubAuthStep runs one GitHub authentication setup step.
func SetupGitHubAuthStep(repoFullName, step string) error {
	requestedStep := step
	step = normalizeGitHubAuthStep(step)
	if step == "" {
		return fmt.Errorf("invalid GitHub auth setup step %q (valid: all, get-token, auth-login, setup-git)", requestedStep)
	}

	log.Printf("Setting up GitHub authentication for repository: %s", repoFullName)

	// Determine GitHub host
	githubHost := getGitHubAuthHost()

	switch step {
	case "get-token":
		token, err := GetGitHubToken(repoFullName)
		if err != nil {
			return fmt.Errorf("failed to get GitHub token: %w", err)
		}
		fmt.Println(token)
		return nil
	case "auth-login":
		token, err := GetGitHubToken(repoFullName)
		if err != nil {
			return fmt.Errorf("failed to get GitHub token: %w", err)
		}

		env := os.Environ()
		if githubHost != "github.com" {
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))
		}

		if err := performGHAuthLogin(githubHost, token, env); err != nil {
			return fmt.Errorf("failed to authenticate gh CLI: %w", err)
		}
		return nil
	case "setup-git":
		env := os.Environ()
		if githubHost != "github.com" {
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))
		}

		if err := performGHAuthSetupGit(githubHost, env); err != nil {
			return fmt.Errorf("failed to setup git authentication: %w", err)
		}
		return nil
	}

	// Check if GITHUB_TOKEN is already set in the environment
	existingToken := os.Getenv("GITHUB_TOKEN")
	if existingToken != "" {
		log.Printf("GITHUB_TOKEN environment variable is already set")

		// Set up environment for git setup
		env := os.Environ()

		// For GitHub Enterprise Server, we need to explicitly authenticate
		// even when GITHUB_TOKEN is set, because gh auth setup-git requires
		// the host to be in the authenticated hosts list
		if githubHost != "github.com" {
			log.Printf("GitHub Enterprise Server detected (%s), performing explicit gh auth login", githubHost)
			env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))

			// Perform gh auth login for GHES
			if err := performGHAuthLogin(githubHost, existingToken, env); err != nil {
				return fmt.Errorf("failed to authenticate gh CLI for GHES: %w", err)
			}
		} else {
			log.Printf("Skipping gh auth login for github.com (GITHUB_TOKEN is automatically used)")
		}

		// Setup git authentication
		if err := performGHAuthSetupGit(githubHost, env); err != nil {
			return fmt.Errorf("failed to setup git authentication: %w", err)
		}

		log.Printf("Successfully set up GitHub authentication using existing GITHUB_TOKEN")
		return nil
	}

	// Get GitHub token for authentication
	token, err := GetGitHubToken(repoFullName)
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Set up environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("GITHUB_TOKEN=%s", token))

	// Add GH_HOST for GitHub Enterprise Server
	if githubHost != "github.com" {
		env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))
	}

	// Authenticate gh CLI
	if err := performGHAuthLogin(githubHost, token, env); err != nil {
		return fmt.Errorf("failed to authenticate gh CLI: %w", err)
	}

	// Setup git authentication
	if err := performGHAuthSetupGit(githubHost, env); err != nil {
		return fmt.Errorf("failed to setup git authentication: %w", err)
	}

	log.Printf("Successfully set up GitHub authentication")
	return nil
}
