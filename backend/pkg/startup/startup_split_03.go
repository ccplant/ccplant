package startup

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func normalizeGitHubAuthStep(step string) string {
	switch strings.ToLower(strings.TrimSpace(step)) {
	case "", "all":
		return "all"
	case "token", "get-token":
		return "get-token"
	case "login", "auth-login":
		return "auth-login"
	case "git", "setup-git":
		return "setup-git"
	default:
		return ""
	}
}

func getGitHubAuthHost() string {
	githubHost := "github.com"
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		githubHost = strings.TrimPrefix(githubAPI, "https://")
		githubHost = strings.TrimPrefix(githubHost, "http://")
		githubHost = strings.TrimSuffix(githubHost, "/api/v3")
	}
	return githubHost
}

// performGHAuthLogin performs gh auth login
func performGHAuthLogin(githubHost, token string, env []string) error {
	log.Printf("Performing gh auth login for host: %s", githubHost)

	var cmd *exec.Cmd
	if githubHost == "github.com" {
		// For GitHub.com, use simple auth login
		cmd = exec.Command("gh", "auth", "login", "--with-token")
	} else {
		// For GitHub Enterprise, specify hostname
		cmd = exec.Command("gh", "auth", "login", "--hostname", githubHost, "--with-token")
	}

	cmd.Stdin = strings.NewReader(token)
	cmd.Env = withoutGitHubTokenEnv(env)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth login failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully authenticated gh CLI for %s", githubHost)
	return nil
}

func withoutGitHubTokenEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GITHUB_TOKEN=") || strings.HasPrefix(entry, "GH_TOKEN=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// performGHAuthSetupGit performs gh auth setup-git
func performGHAuthSetupGit(githubHost string, env []string) error {
	log.Printf("Performing gh auth setup-git for host: %s", githubHost)

	var cmd *exec.Cmd
	if githubHost == "" || githubHost == "github.com" {
		cmd = exec.Command("gh", "auth", "setup-git")
	} else {
		// For GitHub Enterprise Server, specify hostname with --force
		// --force is required when the host might not be in gh's authenticated hosts list
		// (e.g., when using GITHUB_TOKEN environment variable)
		cmd = exec.Command("gh", "auth", "setup-git", "--hostname", githubHost, "--force")
	}
	cmd.Env = env

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth setup-git failed: %w, stderr: %s", err, stderr.String())
	}

	log.Printf("Successfully set up git authentication for %s", githubHost)
	return nil
}
