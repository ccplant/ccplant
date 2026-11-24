package services

import (
	"fmt"
	"log"
	"os"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This consolidates buildEnvVars and envFrom logic into a single unified structure.

func (b *sessionSettingsBuilder) baseEnvironment() {
	m := b.manager
	req := b.request
	session := b.session
	// Build env vars (mirrors buildEnvVars logic from line 2695)
	env := map[string]string{
		proxybinary.EnvName:   m.sessionBinaryPath(),
		"AGENTAPI_PORT":       fmt.Sprintf("%d", m.k8sConfig.BasePort),
		"AGENTAPI_SESSION_ID": session.id,
		"AGENTAPI_USER_ID":    req.UserID,
		"HOME":                "/home/agentapi",
		"GITHUB_APP_PEM_PATH": "/tmp/github-app/app.pem",
	}
	// The parent API resolves Helm-provided GitHub configuration into the
	// SessionSettings sent to dedicated/external session managers. Those
	// managers must not need access to the parent's Kubernetes Secrets.
	githubEnvVars := []string{"GITHUB_API", "GITHUB_URL"}
	if req.GithubToken == "" {
		githubEnvVars = append(githubEnvVars, "GITHUB_APP_ID", "GITHUB_INSTALLATION_ID", "GITHUB_APP_PEM", "REPOSITORY_RESTRICTION")
	}
	for _, envName := range githubEnvVars {
		if value := os.Getenv(envName); value != "" {
			env[envName] = value
		}
	}
	if req.ResumeFrom != "" {
		env["AGENTAPI_RESUME_FROM"] = req.ResumeFrom
	}

	// Add Claude Code telemetry configuration
	if m.k8sConfig.OtelCollectorEnabled {
		env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
		env["OTEL_METRICS_EXPORTER"] = "prometheus"
	}

	// Add Team ID if in team scope
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		env["AGENTAPI_TEAM_ID"] = req.TeamID
	}

	// Add Agent Type if specified
	if req.AgentType != "" {
		env["AGENTAPI_AGENT_TYPE"] = req.AgentType
	}
	b.environment = env
}

func (b *sessionSettingsBuilder) requestEnvironment() {
	env := b.environment
	req := b.request
	// Add CLAUDE_ARGS from request environment or proxy's environment
	claudeArgs := ""
	if req.Environment != nil {
		if v, ok := req.Environment["CLAUDE_ARGS"]; ok {
			claudeArgs = v
		}
	}
	if claudeArgs == "" {
		claudeArgs = os.Getenv("CLAUDE_ARGS")
	}
	if claudeArgs != "" {
		env["CLAUDE_ARGS"] = claudeArgs
	}

	// Add repository info if available
	if req.RepoInfo != nil {
		env["AGENTAPI_REPO_FULLNAME"] = req.RepoInfo.FullName
		env["AGENTAPI_CLONE_DIR"] = "/home/agentapi/workdir/repo"
	}

	// Add environment variables from request (except CLAUDE_ARGS which is already handled)
	for k, v := range req.Environment {
		if k != "CLAUDE_ARGS" {
			env[k] = v
		}
	}

	// Add VAPID environment variables for push notifications
	vapidEnvVars := []string{"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_CONTACT_EMAIL"}
	for _, envName := range vapidEnvVars {
		if value := os.Getenv(envName); value != "" {
			env[envName] = value
		}
	}
}

func (b *sessionSettingsBuilder) secretEnvironment() {
	ctx := b.ctx
	env := b.environment
	m := b.manager
	req := b.request
	// Build list of secret names to expand into env map
	// Pod does not have permission to read secrets, so we need to expand them here
	var secretNames []string

	if req.GithubToken != "" {
		// When params.github_token is provided: embed token directly, no shared authentication secret needed
		env["GITHUB_TOKEN"] = req.GithubToken
	} else if m.k8sConfig.GitHubSecretName != "" {
		secretNames = append(secretNames, m.k8sConfig.GitHubSecretName)
	}
	if m.k8sConfig.GitHubConfigSecretName != "" {
		secretNames = append(secretNames, m.k8sConfig.GitHubConfigSecretName)
	}

	// Expand secrets into env map (GitHub secrets only)
	for _, secretName := range secretNames {
		secret, err := m.client.CoreV1().Secrets(m.namespace).Get(
			ctx,
			secretName,
			metav1.GetOptions{},
		)
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Printf("[K8S_SESSION] Warning: failed to read secret %s for session settings: %v", secretName, err)
			}
			// Skip secrets that don't exist (they are all optional)
			continue
		}

		// Merge secret data into env map (later secrets override earlier ones due to iteration order)
		for k, v := range secret.Data {
			env[k] = string(v)
		}
	}
}

func (b *sessionSettingsBuilder) scopeEnvironment() {
	ctx := b.ctx
	env := b.environment
	m := b.manager
	req := b.request
	// Expand personal API key directly from repository for user-scoped sessions
	// Treat empty scope as user scope (default behavior)
	if (req.Scope == entities.ScopeUser || req.Scope == "") && m.personalAPIKeyRepo != nil {
		apiKey := m.ensurePersonalAPIKey(ctx, req.UserID)
		if apiKey != nil {
			env["AGENTAPI_KEY"] = apiKey.APIKey()
			log.Printf("[K8S_SESSION] Added personal API key to session settings env for user %s", req.UserID)
		}
	}

	// Expand team env vars directly from repository for team-scoped sessions
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && m.teamConfigRepo != nil {
		teamConfig, err := m.teamConfigRepo.FindByTeamID(ctx, req.TeamID)
		if err != nil {
			log.Printf("[K8S_SESSION] Team config not found for team %s in session settings: %v", req.TeamID, err)
		} else {
			if sa := teamConfig.ServiceAccount(); sa != nil {
				env["AGENTAPI_KEY"] = sa.APIKey()
				log.Printf("[K8S_SESSION] Added service account API key to session settings env for team %s", req.TeamID)
			}
			for k, v := range teamConfig.EnvVars() {
				env[k] = v
			}
		}
	}
}

func (b *sessionSettingsBuilder) layeredEnvironment() {
	ctx := b.ctx
	env := b.environment
	m := b.manager
	req := b.request
	session := b.session
	settings := b.settings
	// Resolve and materialize settings from agentapi-settings-* Secrets.
	// This merges env_vars, bedrock credentials, oauth token, MCP servers,
	// marketplaces, plugins, and hooks from base → team → user → oneshot layers.
	materialized := m.resolveSettings(ctx, session, req)
	for k, v := range materialized.EnvVars {
		env[k] = v
	}
	secretEnv, secretFiles := m.resolveProjectedSecrets(ctx, req)
	for k, v := range secretEnv {
		if _, exists := env[k]; exists {
			log.Printf("[K8S_SESSION] Skipping secret environment projection %s because the name is already configured", k)
			continue
		}
		env[k] = v
	}
	m.setTeamGitHubInstallationToken(ctx, req, env)

	// Session profile settings override all team/user settings, including
	// Bedrock-derived environment variables and team credentials.
	for k, v := range req.ProfileEnvironment {
		env[k] = v
	}
	// Explicit session request values remain the highest-priority user input.
	for k, v := range req.Environment {
		env[k] = v
	}

	if req.AgentType == "pi" {
		ensurePiSettingsEnv(env)
	}

	m.injectSciaProxyEnv(env, req)

	// codex-acp explicitly sends its selected sandbox policy with every turn,
	// overriding sandbox_mode from ~/.codex/config.toml. Session Pods are already
	// isolated by agentapi-proxy, so start the adapter in its full-access mode to
	// avoid attempting to create a nested bubblewrap sandbox.
	if req.AgentType == "codex-acp" {
		env["INITIAL_AGENT_MODE"] = "agent-full-access"
	}

	if m.k8sConfig.CLIImage != "" {
		env[proxybinary.EnvName] = sessionCLIPath
	}
	settings.Env = env
	settings.Repositories = append([]sessionsettings.RepositoryConfig(nil), req.ProfileRepositories...)
	settings.UsageReportingEnabled = m.config.Usage.Enabled

	settings.Pi = buildPiConfig(req.PiConnection)
	b.materialized = materialized
	b.secretFiles = secretFiles
}
