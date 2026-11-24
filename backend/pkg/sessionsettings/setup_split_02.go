package sessionsettings

import (
	"log"
	"os"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

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
