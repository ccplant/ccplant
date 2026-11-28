package sessionsettings

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

// SetupOptions configures the Setup behavior.
type SetupOptions struct {
	// InputPath is the path to the session settings YAML file.
	// Defaults to /session-settings/settings.yaml.
	InputPath string

	// CompileOptions controls file generation (Compile step).
	CompileOptions CompileOptions

	// CredentialsFile is the path to the credentials.json mounted from Secret (optional).
	CredentialsFile string

	// NotificationSubscriptions is the source directory for notification subscription files (optional).
	NotificationSubscriptions string

	// NotificationsDir is the destination directory for notification files (optional).
	NotificationsDir string

	// RegisterMarketplaces registers cloned marketplace repos via claude CLI.
	RegisterMarketplaces bool

	// SettingsFile is the path to the user settings.json (from claude-config-user ConfigMap).
	// Contains marketplace and plugin configuration. Optional.
	SettingsFile string

	// PEMOutputPath is where GITHUB_APP_PEM content is written.
	// Defaults to /tmp/github-app/app.pem.
	PEMOutputPath string
}

// DefaultSetupOptions returns the default Setup options.
func DefaultSetupOptions() SetupOptions {
	return SetupOptions{
		InputPath:      "/session-settings/settings.yaml",
		CompileOptions: DefaultCompileOptions(),
		PEMOutputPath:  "/tmp/github-app/app.pem",
	}
}

// Setup runs the full init-container setup sequence for a session Pod:
//  1. write-pem  : writes GITHUB_APP_PEM env var to a file on disk
//  2. clone-repo : clones the repository (if session.repository is set)
//  3. compile    : generates all Claude config files from settings.yaml
//  4. sync-extra : copies credentials, notification subscriptions
func Setup(opts SetupOptions) error {
	if opts.InputPath == "" {
		opts.InputPath = DefaultSetupOptions().InputPath
	}
	if opts.PEMOutputPath == "" {
		opts.PEMOutputPath = DefaultSetupOptions().PEMOutputPath
	}

	log.Printf("[SETUP] Reading settings from %s", opts.InputPath)
	settings, err := LoadSettings(opts.InputPath)
	if err != nil {
		return fmt.Errorf("failed to load session settings: %w", err)
	}
	return SetupSettings(settings, opts)
}

// SetupSettings runs setup from an in-memory settings object. Agent-layer
// callers should use this entry point so environment variables never need an
// intermediate settings file.
func SetupSettings(settings *SessionSettings, opts SetupOptions) error {
	if opts.PEMOutputPath == "" {
		opts.PEMOutputPath = DefaultSetupOptions().PEMOutputPath
	}
	if settings.Env == nil {
		settings.Env = make(map[string]string)
	}
	homeDir := strings.TrimSpace(opts.CompileOptions.OutputDir)
	if homeDir == "" {
		homeDir = DefaultCompileOptions().OutputDir
	}
	settings.Env["HOME"] = homeDir

	// 1. Write GitHub App PEM to disk so git/gh can use it
	if err := writePEM(settings, opts.PEMOutputPath); err != nil {
		// Non-fatal: not all sessions use GitHub App auth
		log.Printf("[SETUP] Warning: write-pem skipped: %v", err)
	}

	// 2. Configure broker-backed GitHub authentication even when no repository
	// is preloaded. Profiles may allow repositories that the agent clones later.
	if err := ConfigureGitHubTokenBroker(settings); err != nil {
		return fmt.Errorf("configure GitHub token broker: %w", err)
	}

	// Clone repository if configured.
	if (settings.Repository != nil && settings.Repository.FullName != "") || len(settings.Repositories) > 0 {
		if settings.Repository != nil && settings.Repository.FullName != "" {
			if err := cloneRepository(settings, settings.Repository); err != nil {
				return fmt.Errorf("clone-repo failed: %w", err)
			}
		}
		for i := range settings.Repositories {
			if err := cloneRepository(settings, &settings.Repositories[i]); err != nil {
				return fmt.Errorf("clone preloaded repository %s: %w", settings.Repositories[i].FullName, err)
			}
		}
	}

	// 3. Compile in-memory settings → config files
	if err := CompileSettings(settings, opts.CompileOptions); err != nil {
		return fmt.Errorf("compile failed: %w", err)
	}

	outputDir := opts.CompileOptions.OutputDir
	if outputDir == "" {
		outputDir = DefaultCompileOptions().OutputDir
	}
	// 4. Copy credentials, CLAUDE.md, notification subscriptions
	if err := syncExtra(settings, opts); err != nil {
		return fmt.Errorf("sync-extra failed: %w", err)
	}

	// 5. Re-patch ~/.claude.json to ensure onboarding fields are set.
	//    The sync step (marketplace clone / plugin install) may trigger Claude
	//    CLI which rewrites ~/.claude.json and drops bypassPermissionsModeAccepted,
	//    causing the "Welcome to Claude Code" screen on next launch.
	if err := patchClaudeJSON(outputDir, settings.Claude.ClaudeJSON); err != nil {
		log.Printf("[SETUP] Warning: failed to re-patch .claude.json: %v", err)
	}

	log.Printf("[SETUP] Setup completed successfully")
	return nil
}

func configureGitHubTokenBroker(settings *SessionSettings) error {
	brokerURL := strings.TrimSpace(settings.Env["AGENTAPI_GITHUB_BROKER_URL"])
	brokerToken := strings.TrimSpace(settings.Env["AGENTAPI_GITHUB_BROKER_TOKEN"])
	brokerSocket := strings.TrimSpace(settings.Env["AGENTAPI_GITHUB_BROKER_SOCKET"])
	if brokerURL == "" && brokerToken == "" && brokerSocket == "" {
		return nil
	}
	if brokerSocket == "" && (brokerURL == "" || brokerToken == "") {
		return fmt.Errorf("broker URL and token must both be configured")
	}
	// A broker lease is the authoritative credential for this session. Remove
	// inherited direct credentials so a pre-warmed runner cannot fall back to a
	// deleted built-in GitHub App or a stale token before invoking the broker
	// wrappers. Keep GITHUB_API, GITHUB_URL, and GH_HOST for host routing.
	for _, name := range []string{
		"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
		"GITHUB_PERSONAL_ACCESS_TOKEN", "GITHUB_APP_ID", "GITHUB_INSTALLATION_ID",
		"GITHUB_APP_PEM", "GITHUB_APP_PEM_PATH", "REPOSITORY_RESTRICTION",
	} {
		delete(settings.Env, name)
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset direct GitHub credential %s: %w", name, err)
		}
	}
	log.Printf("[GITHUB_BROKER] Configuring broker-backed GitHub authentication for host %s", githubHostForLog(settings.Env))
	homeDir := strings.TrimSpace(settings.Env["HOME"])
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	binDir := filepath.Join(homeDir, ".local", "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return err
	}
	helperPath := filepath.Join(binDir, "agentapi-github-credential")
	helper := `#!/bin/sh
set -eu
[ "${1:-get}" = "get" ] || exit 0
broker_url="${AGENTAPI_GITHUB_BROKER_URL:-http://localhost/github-credentials}"
repository=""
while IFS='=' read -r key value; do
  [ "$key" = "path" ] && repository="${value%.git}"
done
if [ -n "$repository" ]; then
	printf '%s\n' "[GITHUB_BROKER] Requesting git credential for repository=$repository" >&2
  if [ -n "${AGENTAPI_GITHUB_BROKER_SOCKET:-}" ]; then
    response="$(curl -fsS --unix-socket "$AGENTAPI_GITHUB_BROKER_SOCKET" -G --data-urlencode "repository=$repository" "$broker_url")"
  else
    response="$(curl -fsS -G --data-urlencode "repository=$repository" -H "Authorization: Bearer $AGENTAPI_GITHUB_BROKER_TOKEN" "$AGENTAPI_GITHUB_BROKER_URL")"
  fi
else
	printf '%s\n' "[GITHUB_BROKER] Requesting git credential for default repository" >&2
  if [ -n "${AGENTAPI_GITHUB_BROKER_SOCKET:-}" ]; then
    response="$(curl -fsS --unix-socket "$AGENTAPI_GITHUB_BROKER_SOCKET" "$broker_url")"
  else
    response="$(curl -fsS -H "Authorization: Bearer $AGENTAPI_GITHUB_BROKER_TOKEN" "$AGENTAPI_GITHUB_BROKER_URL")"
  fi
fi
token="$(printf '%s' "$response" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
[ -n "$token" ] || exit 1
printf 'username=x-access-token\npassword=%s\n\n' "$token"
`
	if err := os.WriteFile(helperPath, []byte(helper), 0700); err != nil {
		return err
	}
	cmd := exec.Command("git", "config", "--global", "credential.helper", helperPath)
	cmd.Env = append(githubCommandEnv(), "HOME="+homeDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git credential helper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	cmd = exec.Command("git", "config", "--global", "credential.useHttpPath", "true")
	cmd.Env = append(githubCommandEnv(), "HOME="+homeDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git credential useHttpPath: %w: %s", err, strings.TrimSpace(string(output)))
	}

	realGH, err := exec.LookPath("gh")
	if err == nil && !strings.HasPrefix(realGH, binDir+string(filepath.Separator)) {
		wrapperPath := filepath.Join(binDir, "gh")
		wrapper := fmt.Sprintf(`#!/bin/sh
set -eu
broker_url="${AGENTAPI_GITHUB_BROKER_URL:-http://localhost/github-credentials}"
repository=""
if [ "${1:-}" = "repo" ] && [ "${2:-}" = "clone" ] && [ -n "${3:-}" ]; then
  repository="${3#https://*/}"
  repository="${repository%%/}"
  repository="${repository%%.git}"
fi
if [ -n "$repository" ]; then
  if [ -n "${AGENTAPI_GITHUB_BROKER_SOCKET:-}" ]; then
    response="$(curl -fsS --unix-socket "$AGENTAPI_GITHUB_BROKER_SOCKET" -G --data-urlencode "repository=$repository" "$broker_url")"
  else
    response="$(curl -fsS -G --data-urlencode "repository=$repository" -H "Authorization: Bearer $AGENTAPI_GITHUB_BROKER_TOKEN" "$AGENTAPI_GITHUB_BROKER_URL")"
  fi
else
  if [ -n "${AGENTAPI_GITHUB_BROKER_SOCKET:-}" ]; then
    response="$(curl -fsS --unix-socket "$AGENTAPI_GITHUB_BROKER_SOCKET" "$broker_url")"
  else
    response="$(curl -fsS -H "Authorization: Bearer $AGENTAPI_GITHUB_BROKER_TOKEN" "$AGENTAPI_GITHUB_BROKER_URL")"
  fi
fi
token="$(printf '%%s' "$response" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
[ -n "$token" ] || exit 1
printf '%%s\n' "[GITHUB_BROKER] Invoking gh with broker credential host=${GH_HOST:-github.com} repository=${repository:-<default>}" >&2
GH_TOKEN="$token" GH_ENTERPRISE_TOKEN="$token" exec %q "$@"
`, realGH)
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0700); err != nil {
			return err
		}
		currentPath := settings.Env["PATH"]
		if currentPath == "" {
			currentPath = os.Getenv("PATH")
		}
		settings.Env["PATH"] = binDir + string(os.PathListSeparator) + currentPath
	}
	log.Printf("[GITHUB_BROKER] Broker-backed GitHub authentication configured for host %s", githubHostForLog(settings.Env))
	return nil
}

// ConfigureGitHubTokenBroker installs the session-scoped gh wrapper and Git
// credential helper. Provisioners may call it again after restoring managed
// files because a persisted .gitconfig can otherwise replace the helper entry.
func ConfigureGitHubTokenBroker(settings *SessionSettings) error {
	return configureGitHubTokenBroker(settings)
}

func githubHostForLog(env map[string]string) string {
	if host := strings.TrimSpace(env["GH_HOST"]); host != "" {
		return host
	}
	if apiURL := strings.TrimSpace(env["GITHUB_API"]); apiURL != "" {
		host := strings.TrimPrefix(apiURL, "https://")
		host = strings.TrimPrefix(host, "http://")
		return strings.TrimSuffix(host, "/api/v3")
	}
	return "github.com"
}

// writePEM writes the GITHUB_APP_PEM value from Env to a file on disk.
// This replaces the write-pem init container.
func writePEM(settings *SessionSettings, pemOutputPath string) error {
	pem := settings.Env["GITHUB_APP_PEM"]
	if pem == "" {
		return fmt.Errorf("GITHUB_APP_PEM is not set in session env, skipping")
	}

	pemDir := filepath.Dir(pemOutputPath)
	if err := os.MkdirAll(pemDir, 0700); err != nil {
		return fmt.Errorf("failed to create PEM directory: %w", err)
	}

	if err := os.WriteFile(pemOutputPath, []byte(pem), 0600); err != nil {
		return fmt.Errorf("failed to write PEM file: %w", err)
	}

	log.Printf("[SETUP] Wrote GitHub App PEM to %s", pemOutputPath)
	return nil
}

// cloneRepo sets up GitHub auth and clones the repository using gh repo clone
// so that GITHUB_TOKEN / GitHub App auth and GitHub Enterprise Server host
// routing work correctly.
func cloneRepository(settings *SessionSettings, repo *RepositoryConfig) error {

	// Set environment variables from session env so git/gh tools pick them up.
	for k, v := range setupProcessEnv(settings.Env) {
		if err := os.Setenv(k, v); err != nil {
			log.Printf("[SETUP] Warning: failed to set env %s: %v", k, err)
		}
	}

	log.Printf("[SETUP] Setting up GitHub auth for repo: %s", repo.FullName)
	if err := startup.SetupGitHubAuth(repo.FullName); err != nil {
		// Non-fatal for public repos
		log.Printf("[SETUP] Warning: GitHub auth setup failed: %v", err)
	}

	cloneDir := repo.CloneDir
	if cloneDir == "" {
		cloneDir = filepath.Join("/home/agentapi/workdir", repo.FullName)
	}

	// Skip clone if already present
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err == nil {
		if settings.Restart || settings.WorkspaceKept {
			return nil
		}
		log.Printf("[SETUP] Repository already cloned at %s, skipping", cloneDir)
	} else {
		if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
			return fmt.Errorf("failed to create parent dir for clone: %w", err)
		}

		log.Printf("[SETUP] Cloning %s into %s via gh repo clone", repo.FullName, cloneDir)
		cmd := exec.Command("gh", "repo", "clone", repo.FullName, cloneDir, "--", "--depth", "1")
		cmd.Env = githubCommandEnv()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// Fallback: try GITHUB_URL-based git clone for non-GitHub hosts
			githubURL := github_pkg.GetGitHubURL()
			cloneURL := fmt.Sprintf("%s/%s.git", githubURL, repo.FullName)
			log.Printf("[SETUP] gh repo clone failed, falling back to git clone: %s", cloneURL)
			cmd2 := exec.Command("git", "clone", "--depth", "1", cloneURL, cloneDir)
			cmd2.Env = githubCommandEnv()
			cmd2.Stdout = os.Stdout
			cmd2.Stderr = os.Stderr
			if err2 := cmd2.Run(); err2 != nil {
				return fmt.Errorf("git clone failed: %w", err2)
			}
		}

		log.Printf("[SETUP] Cloned %s to %s", repo.FullName, cloneDir)
	}

	if err := checkoutRepositoryTarget(repo, cloneDir); err != nil {
		return err
	}

	return nil
}

func githubCommandEnv() []string {
	env := os.Environ()
	if githubAPI := os.Getenv("GITHUB_API"); githubAPI != "" && githubAPI != "https://api.github.com" {
		githubHost := strings.TrimPrefix(githubAPI, "https://")
		githubHost = strings.TrimPrefix(githubHost, "http://")
		githubHost = strings.TrimSuffix(githubHost, "/api/v3")
		env = append(env, fmt.Sprintf("GH_HOST=%s", githubHost))
		log.Printf("[SETUP] GHE detected, setting GH_HOST=%s for gh/git", githubHost)
	}
	return env
}

func checkoutRepositoryTarget(repo *RepositoryConfig, cloneDir string) error {
	if repo == nil {
		return nil
	}

	targetType, targetValue := repositoryCheckoutTarget(repo)
	switch targetType {
	case "pr":
		pr := targetValue
		log.Printf("[SETUP] Checking out PR %s for %s", pr, repo.FullName)
		cmd := exec.Command("gh", "pr", "checkout", pr, "--detach")
		cmd.Dir = cloneDir
		cmd.Env = githubCommandEnv()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to checkout PR %s: %w", pr, err)
		}
		return nil
	case "branch":
		branch := targetValue
		log.Printf("[SETUP] Checking out branch %s for %s", branch, repo.FullName)
		fetch := exec.Command("git", "fetch", "origin", branch, "--depth", "1")
		fetch.Dir = cloneDir
		fetch.Env = githubCommandEnv()
		fetch.Stdout = os.Stdout
		fetch.Stderr = os.Stderr
		if err := fetch.Run(); err != nil {
			return fmt.Errorf("failed to fetch branch %s: %w", branch, err)
		}

		checkout := exec.Command("git", "checkout", "-B", branch, "FETCH_HEAD")
		checkout.Dir = cloneDir
		checkout.Env = githubCommandEnv()
		checkout.Stdout = os.Stdout
		checkout.Stderr = os.Stderr
		if err := checkout.Run(); err != nil {
			return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
		}
	}

	return nil
}

func repositoryCheckoutTarget(repo *RepositoryConfig) (string, string) {
	if repo == nil {
		return "", ""
	}
	if pr := strings.TrimSpace(repo.PR); pr != "" {
		return "pr", pr
	}
	if branch := strings.TrimSpace(repo.Branch); branch != "" {
		return "branch", branch
	}
	return "", ""
}

// syncExtra handles credentials, notification subscriptions,
// and marketplace/plugin registration.
// ~/.claude.json and ~/.claude/settings.json are already written by the compile step.
func syncExtra(settings *SessionSettings, opts SetupOptions) error {
	outputDir := opts.CompileOptions.OutputDir
	if outputDir == "" {
		outputDir = DefaultCompileOptions().OutputDir
	}

	// Set env so marketplace clone / claude CLI picks up GITHUB_TOKEN etc.
	for k, v := range setupProcessEnv(settings.Env) {
		if err := os.Setenv(k, v); err != nil {
			log.Printf("[SETUP] Warning: failed to set env %s: %v", k, err)
		}
	}

	syncOpts := startup.SyncOptions{
		OutputDir:                 outputDir,
		CredentialsFile:           opts.CredentialsFile,
		NotificationSubscriptions: opts.NotificationSubscriptions,
		NotificationsDir:          opts.NotificationsDir,
		RegisterMarketplaces:      opts.RegisterMarketplaces,
		AgentType:                 settings.Session.AgentType,
	}

	return startup.Sync(syncOpts)
}

func setupProcessEnv(env map[string]string) map[string]string {
	filtered := make(map[string]string, len(env))
	for k, v := range env {
		switch k {
		case "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
			"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "NODE_EXTRA_CA_CERTS",
			"AGENTAPI_SCIA_PROXY_URL", "AGENTAPI_SCIA_GOOGLE_CREDENTIAL", "AGENTAPI_SCIA_USER_NAMESPACE":
			continue
		default:
			filtered[k] = v
		}
	}
	return filtered
}
