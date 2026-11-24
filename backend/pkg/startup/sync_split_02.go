package startup

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

type claudeSettingsJSON struct {
	EnabledPlugins []string                    `json:"enabled_plugins"`
	Marketplaces   map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	Skills         []string                    `json:"skills,omitempty"`
}

func installSkillsPackages(outputDir, agent string, packages []string) error {
	for _, configured := range packages {
		source, skill, err := parseSkillsPackage(configured)
		if err != nil {
			return err
		}
		if source == "" {
			continue
		}
		cmd := exec.Command(skillsBinPath, "add", source, "--agent", agent, "--skill", skill, "--global", "--copy", "--yes")
		cmd.Env = append(os.Environ(), "HOME="+outputDir, "DISABLE_TELEMETRY=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("install skills package %s for %s: %w: %s", source, agent, err, strings.TrimSpace(string(output)))
		}
		log.Printf("[SYNC] Installed skill %s from package %s for %s", skill, source, agent)
	}
	return nil
}

// parseSkillsPackage accepts either a legacy package source (which installs all
// skills) or "<source> --skill <name>". It deliberately does not invoke a shell.
func parseSkillsPackage(configured string) (source, skill string, err error) {
	fields := strings.Fields(configured)
	if len(fields) == 0 {
		return "", "", nil
	}
	if len(fields) == 1 {
		source, skill = fields[0], "*"
	} else if len(fields) == 3 && fields[1] == "--skill" {
		source, skill = fields[0], fields[2]
	} else {
		return "", "", fmt.Errorf("invalid skills package %q: expected <source> [--skill <name>]", configured)
	}
	if strings.HasPrefix(source, "-") || strings.HasPrefix(skill, "-") ||
		strings.ContainsAny(source, "\x00\r\n") || strings.ContainsAny(skill, "\x00\r\n") {
		return "", "", fmt.Errorf("invalid skills package %q", configured)
	}
	return source, skill, nil
}

// readClaudeSettingsJSON reads enabled_plugins and marketplaces from the
// settings.json file written by the compile step.
func readClaudeSettingsJSON(settingsPath string) (*claudeSettingsJSON, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	var content claudeSettingsJSON
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}

	return &content, nil
}

// getGHESHost returns the GitHub Enterprise Server hostname extracted from
// the GITHUB_API environment variable. Returns an empty string when running
// against github.com (i.e. GITHUB_API is unset or equals "https://api.github.com").
func getGHESHost() string {
	githubAPI := os.Getenv("GITHUB_API")
	if githubAPI == "" || githubAPI == "https://api.github.com" {
		return ""
	}
	host := strings.TrimPrefix(githubAPI, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/api/v3")
	return host
}

// buildGitHubEnvForHost builds environment variables for GitHub CLI/git commands.
// GH_HOST is set to the GHES hostname only when targetHost matches the configured
// GHES host. For github.com target hosts in a GHES environment, GH_HOST is left
// unset so that gh CLI defaults to github.com.
func buildGitHubEnvForHost(targetHost string) []string {
	env := os.Environ()
	ghesHost := getGHESHost()
	if ghesHost != "" && targetHost == ghesHost {
		env = append(env, fmt.Sprintf("GH_HOST=%s", ghesHost))
		log.Printf("[SYNC] GHE detected, setting GH_HOST=%s", ghesHost)
	}
	return env
}

// cloneMarketplace clones a marketplace repository.
//
// Routing strategy:
//   - github.com URL in a GHES environment: uses cloneGitHubDotComMarketplace
//     so that GH_HOST is NOT set to the enterprise server. Public repos are
//     cloned without authentication; private repos require GITHUB_COM_TOKEN.
//   - GitHub URL (github.com outside GHES, or a GHES-hosted URL): uses
//     `gh repo clone` with GH_HOST set appropriately so that token auth and
//     GHE host routing work correctly.
//   - Other URLs (e.g., local file paths used in tests): falls back to plain
//     `git clone`.
//
// If the target directory already contains a git repo it pulls updates instead.
func cloneMarketplace(url, targetDir string) error {
	urlHost := github_pkg.ExtractRepositoryHostname(url)
	ghesHost := getGHESHost()
	repoFullName := github_pkg.ParseRepositoryURL(url)

	// Preserve the explicit GitHub.com credential used when the built-in App is
	// configured for GHES rather than github.com.
	if repoFullName != "" && urlHost == "github.com" && ghesHost != "" && os.Getenv("GITHUB_COM_TOKEN") != "" {
		return cloneGitHubDotComMarketplace(repoFullName, targetDir)
	}

	var marketplaceToken string
	if repoFullName != "" {
		token, err := GenerateMarketplaceGitHubAppToken(context.Background(), url)
		if err != nil {
			log.Printf("[SYNC] Built-in GitHub App authentication unavailable for marketplace %s: %v; trying anonymous clone", repoFullName, err)
		} else {
			marketplaceToken = token
		}
	}

	if _, err := os.Stat(filepath.Join(targetDir, ".git")); err == nil {
		log.Printf("[SYNC] Marketplace already cloned at %s, pulling updates", targetDir)
		output, err := runMarketplaceGit(targetDir, marketplaceToken, "pull")
		if err != nil {
			return fmt.Errorf("git pull failed: %w, output: %s", err, string(output))
		}
		return nil
	}

	if repoFullName != "" {
		log.Printf("[SYNC] Cloning GitHub marketplace %s via HTTPS git", repoFullName)
		parentDir := filepath.Dir(targetDir)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}
		output, err := runMarketplaceGit(parentDir, marketplaceToken, "clone", "--depth", "1", url, targetDir)
		if err != nil {
			return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
		}
		return nil
	}

	// Non-GitHub URL (e.g., local path): fall back to git clone.
	log.Printf("[SYNC] Cloning marketplace via git clone: %s", url)
	cmd := exec.Command("git", "clone", "--depth", "1", url, targetDir)
	cmd.Env = marketplaceAnonymousEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
	}
	return nil
}

func runMarketplaceGit(dir, token string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"-c", "credential.helper="}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = dir
	if token == "" {
		cmd.Env = marketplaceAnonymousEnv()
		return cmd.CombinedOutput()
	}

	helperDir, err := os.MkdirTemp("", "agentapi-marketplace-credential-")
	if err != nil {
		return nil, fmt.Errorf("create temporary credential helper: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(helperDir); removeErr != nil {
			log.Printf("[SYNC] Warning: failed to remove temporary marketplace credential helper: %v", removeErr)
		}
	}()
	helperPath := filepath.Join(helperDir, "askpass.sh")
	helper := `#!/bin/sh
case "$1" in
  *Username*) printf '%s' "$AGENTAPI_MARKETPLACE_GIT_USERNAME" ;;
  *) printf '%s' "$AGENTAPI_MARKETPLACE_GIT_PASSWORD" ;;
esac
`
	if err := os.WriteFile(helperPath, []byte(helper), 0700); err != nil {
		return nil, fmt.Errorf("write temporary credential helper: %w", err)
	}
	cmd.Env = append(marketplaceAnonymousEnv(),
		"GIT_ASKPASS="+helperPath,
		"GIT_TERMINAL_PROMPT=0",
		"AGENTAPI_MARKETPLACE_GIT_USERNAME=x-access-token",
		"AGENTAPI_MARKETPLACE_GIT_PASSWORD="+token,
	)
	return cmd.CombinedOutput()
}

func marketplaceAnonymousEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GITHUB_TOKEN=") || strings.HasPrefix(entry, "GH_TOKEN=") ||
			strings.HasPrefix(entry, "GIT_ASKPASS=") || strings.HasPrefix(entry, "GIT_TERMINAL_PROMPT=") ||
			strings.HasPrefix(entry, "AGENTAPI_MARKETPLACE_GIT_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

// cloneGitHubDotComMarketplace clones a marketplace from github.com in a GHES
// environment without setting GH_HOST to the enterprise server.
//
// For private repositories, set GITHUB_COM_TOKEN to a GitHub.com personal
// access token or fine-grained token. Public repositories are cloned without
// authentication using a plain HTTPS git clone.
func cloneGitHubDotComMarketplace(repoFullName, targetDir string) error {
	parentDir := filepath.Dir(targetDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Attempt authenticated clone when GITHUB_COM_TOKEN is set (private repos).
	if token := os.Getenv("GITHUB_COM_TOKEN"); token != "" {
		log.Printf("[SYNC] Cloning github.com marketplace %s via gh repo clone with GITHUB_COM_TOKEN", repoFullName)
		ghEnv := append(os.Environ(), fmt.Sprintf("GITHUB_TOKEN=%s", token))
		cmd := exec.Command("gh", "repo", "clone", repoFullName, targetDir)
		cmd.Env = ghEnv
		cmd.Dir = parentDir
		if output, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			log.Printf("[SYNC] gh repo clone with GITHUB_COM_TOKEN failed for %s: %v, output: %s; falling back to unauthenticated git clone", repoFullName, err, string(output))
		}
	}

	// Fall back to unauthenticated git clone (works for public repos).
	cloneURL := fmt.Sprintf("https://github.com/%s.git", repoFullName)
	log.Printf("[SYNC] Cloning github.com marketplace %s via git clone (unauthenticated)", repoFullName)
	cmd := exec.Command("git", "clone", "--depth", "1", cloneURL, targetDir)
	cmd.Env = os.Environ()
	cmd.Dir = parentDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
	}
	return nil
}

// readMarketplaceName reads the real marketplace name from .claude-plugin/marketplace.json
func readMarketplaceName(targetDir string) (string, error) {
	jsonPath := filepath.Join(targetDir, ".claude-plugin", "marketplace.json")

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to read marketplace.json: %w", err)
	}

	var mp marketplacePluginJSON
	if err := json.Unmarshal(data, &mp); err != nil {
		return "", fmt.Errorf("failed to parse marketplace.json: %w", err)
	}

	if mp.Name == "" {
		return "", fmt.Errorf("marketplace.json has empty name field")
	}

	return mp.Name, nil
}

// resolvePluginName replaces alias marketplace name with real name in plugin identifier
// plugin format: "plugin-name@marketplace-name"
func resolvePluginName(plugin string, nameMapping map[string]string) string {
	parts := strings.SplitN(plugin, "@", 2)
	if len(parts) != 2 {
		return plugin
	}

	pluginName := parts[0]
	marketplaceAlias := parts[1]

	if realName, ok := nameMapping[marketplaceAlias]; ok {
		return pluginName + "@" + realName
	}

	return plugin
}

// officialMarketplace is the official Anthropic marketplace repository
const officialMarketplace = "anthropics/claude-plugins-official"

// claudeBinPath is the Claude Agent SDK executable used by the ACP adapter.
const claudeBinPath = "/home/agentapi/.npm-global/lib/node_modules/@anthropic-ai/claude-agent-sdk/claude"

var skillsBinPath = "/home/agentapi/.npm-global/bin/skills"

// runClaudeCLI executes a claude CLI command with HOME set to outputDir.
func runClaudeCLI(outputDir string, args ...string) error {
	startedAt := time.Now()
	log.Printf("[SYNC] Running claude %s", strings.Join(args, " "))
	cmd := exec.Command(claudeBinPath, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", outputDir))
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[SYNC] claude %s failed after %s: %v", strings.Join(args, " "), time.Since(startedAt).Round(time.Millisecond), err)
		return fmt.Errorf("claude %s failed: %w, output: %s", args[0], err, string(output))
	}
	log.Printf("[SYNC] claude %s completed in %s", strings.Join(args, " "), time.Since(startedAt).Round(time.Millisecond))
	return nil
}

// registerOfficialMarketplace registers the official Anthropic marketplace
func registerOfficialMarketplace(outputDir string) error {
	log.Printf("[SYNC] Registering official marketplace: %s", officialMarketplace)
	if err := runClaudeCLI(outputDir, "plugin", "marketplace", "add", officialMarketplace); err != nil {
		return err
	}
	log.Printf("[SYNC] Successfully registered official marketplace: %s", officialMarketplace)
	return nil
}

// registerMarketplaces uses claude CLI to register cloned marketplaces
func registerMarketplaces(outputDir string, marketplacesDir string) error {
	entries, err := os.ReadDir(marketplacesDir)
	if err != nil {
		return fmt.Errorf("failed to read marketplaces directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}

		marketplacePath := filepath.Join(marketplacesDir, entry.Name())
		log.Printf("[SYNC] Registering marketplace at %s", marketplacePath)
		if err := runClaudeCLI(outputDir, "plugin", "marketplace", "add", marketplacePath); err != nil {
			log.Printf("[SYNC] Warning: failed to register marketplace at %s: %v", marketplacePath, err)
			continue
		}
		log.Printf("[SYNC] Successfully registered marketplace at %s", marketplacePath)
	}
	return nil
}

// installPlugin installs a plugin using claude CLI.
// pluginIdentifier is in "plugin-name@marketplace-name" format.
