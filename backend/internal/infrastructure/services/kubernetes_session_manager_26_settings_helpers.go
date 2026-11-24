package services

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

func profileSystemPromptFile(agentType, content string) *sessionsettings.ManagedFile {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	var path string
	switch agentType {
	case "claude-acp", "claude-legacy":
		path = "/home/agentapi/.claude/CLAUDE.md"
	case "codex-acp":
		path = "/home/agentapi/.codex/AGENTS.md"
	case "pi":
		path = "/home/agentapi/.pi/agent/AGENTS.md"
	case "cursor", "antigravity":
		path = "/home/agentapi/workdir/repo/AGENTS.md"
	default:
		return nil
	}
	return &sessionsettings.ManagedFile{Path: path, Content: content, Permissions: "0644"}
}

func filterManagedStoragesForAgent(storages []entities.ResolvedStorageAttachment, agentType string) []entities.ResolvedStorageAttachment {
	agent := ""
	if strings.Contains(agentType, "codex") {
		agent = "codex"
	} else if strings.Contains(agentType, "claude") {
		agent = "claude"
	}
	result := make([]entities.ResolvedStorageAttachment, 0, len(storages))
	for _, storage := range storages {
		if storage.Managed && strings.HasPrefix(storage.StoragePath, "session-state/") && !strings.HasPrefix(storage.StoragePath, "session-state/acp/") {
			if (strings.Contains(storage.StoragePath, "/codex") && agent != "codex") || (strings.Contains(storage.StoragePath, "/claude") && agent != "claude") {
				continue
			}
		}
		if storage.Managed && strings.HasPrefix(storage.StoragePath, "session-state/acp/") && !strings.HasSuffix(storage.StoragePath, "/"+agent) {
			continue
		}
		result = append(result, storage)
	}
	return result
}

func (m *KubernetesSessionManager) setTeamGitHubInstallationToken(
	ctx context.Context,
	req *entities.RunServerRequest,
	env map[string]string,
) {
	if req.Scope != entities.ScopeTeam || req.TeamID == "" || m.settingsRepo == nil {
		return
	}

	teamSettings, err := m.settingsRepo.FindByName(ctx, req.TeamID)
	if err != nil {
		return
	}
	installationID := teamSettings.GitHubAppInstallationID()
	if installationID == "" {
		return
	}
	env["GITHUB_INSTALLATION_ID"] = installationID

	appID := env["GITHUB_APP_ID"]
	pem := env["GITHUB_APP_PEM"]
	if appID == "" || pem == "" {
		delete(env, "GITHUB_TOKEN")
		log.Printf("[K8S_SESSION] Warning: team %s has a GitHub App installation ID but App credentials are unavailable", req.TeamID)
		return
	}

	repoFullName := ""
	if req.RepoInfo != nil {
		repoFullName = req.RepoInfo.FullName
	}
	token, err := startup.GenerateGitHubAppTokenFromPEM(appID, installationID, []byte(pem), repoFullName)
	if err != nil {
		delete(env, "GITHUB_TOKEN")
		log.Printf("[K8S_SESSION] Warning: failed to generate GitHub App token for team %s: %v", req.TeamID, err)
		return
	}

	env["GITHUB_TOKEN"] = token
	log.Printf("[K8S_SESSION] Added GitHub App installation token to session settings for team %s", req.TeamID)
}

func buildPiConfig(connection *modelprovider.Connection) sessionsettings.PiConfig {
	if connection == nil || !connection.Compatible() {
		return sessionsettings.PiConfig{}
	}
	contextWindow := int64(128000)
	if connection.ContextWindow != nil {
		contextWindow = *connection.ContextWindow
	}
	model := map[string]interface{}{
		"id":            connection.Model,
		"name":          connection.Model,
		"reasoning":     false,
		"input":         []interface{}{"text"},
		"contextWindow": contextWindow,
		"maxTokens":     16384,
	}
	providerConfig := map[string]interface{}{
		"baseUrl": connection.BaseURL,
		"api":     "openai-completions",
		"models":  []interface{}{model},
	}
	if connection.Authentication == "api_key" {
		providerConfig["apiKey"] = connection.APIKey
	} else {
		// Pi requires a credential value even for endpoints without auth.
		providerConfig["apiKey"] = "none"
	}
	return sessionsettings.PiConfig{
		SettingsJSON: map[string]interface{}{
			"defaultProvider": "ccplant",
			"defaultModel":    connection.Model,
		},
		ModelsJSON: map[string]interface{}{
			"providers": map[string]interface{}{
				"ccplant": providerConfig,
			},
		},
	}
}

func (m *KubernetesSessionManager) injectSciaProxyEnv(env map[string]string, req *entities.RunServerRequest) {
	if m.config == nil || !m.config.Scia.Enabled {
		return
	}

	scia := m.config.Scia
	if !m.sciaSessionSidecarEnabled(req) {
		if scia.Credential != "" {
			env["AGENTAPI_SCIA_GOOGLE_CREDENTIAL"] = scia.Credential
		}
		if scia.TodoistCredential != "" {
			env["AGENTAPI_SCIA_TODOIST_CREDENTIAL"] = scia.TodoistCredential
		}
		if scia.UserNamespace != "" {
			env["AGENTAPI_SCIA_USER_NAMESPACE"] = scia.UserNamespace
		}
		if scia.PublicBaseURL != "" {
			env["AGENTAPI_SCIA_PUBLIC_BASE_URL"] = scia.PublicBaseURL
		}
		return
	}

	port := scia.SessionSidecarPort
	if port == 0 {
		port = 18081
	}
	userNamespace := scia.UserNamespace
	if userNamespace == "" {
		userNamespace = sanitizeSciaDynamicUserID(env["AGENTAPI_USER_ID"])
	}
	credential := scia.Credential
	if credential == "" && userNamespace != "" {
		credential = userNamespace + ".google"
	}
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	env["AGENTAPI_SCIA_PROXY_URL"] = proxyURL
	env["HTTP_PROXY"] = proxyURL
	env["HTTPS_PROXY"] = proxyURL
	env["http_proxy"] = proxyURL
	env["https_proxy"] = proxyURL
	env["NO_PROXY"] = mergeNoProxy(mergeNoProxy(sciaNoProxyBase, env["NO_PROXY"]), scia.NoProxy)
	env["no_proxy"] = mergeNoProxy(mergeNoProxy(sciaNoProxyBase, env["no_proxy"]), scia.NoProxy)
	env["SSL_CERT_FILE"] = sciaCABundlePath
	env["REQUESTS_CA_BUNDLE"] = sciaCABundlePath
	env["CURL_CA_BUNDLE"] = sciaCABundlePath
	env["GIT_SSL_CAINFO"] = sciaCABundlePath
	env["NODE_EXTRA_CA_CERTS"] = sciaCAPath
	if credential != "" {
		env["AGENTAPI_SCIA_GOOGLE_CREDENTIAL"] = credential
	}
	todoistCredential := scia.TodoistCredential
	if todoistCredential == "" && userNamespace != "" {
		todoistCredential = userNamespace + ".todoist"
	}
	if todoistCredential != "" {
		env["AGENTAPI_SCIA_TODOIST_CREDENTIAL"] = todoistCredential
	}
	if userNamespace != "" {
		env["AGENTAPI_SCIA_USER_NAMESPACE"] = userNamespace
	}
	if scia.PublicBaseURL != "" {
		env["AGENTAPI_SCIA_PUBLIC_BASE_URL"] = scia.PublicBaseURL
	}
}

func mergeNoProxy(existing, extra string) string {
	seen := map[string]bool{}
	values := make([]string, 0)
	for _, list := range []string{existing, extra} {
		for _, part := range strings.Split(list, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			values = append(values, part)
		}
	}
	return strings.Join(values, ",")
}

func (m *KubernetesSessionManager) resolveProjectedSecrets(ctx context.Context, req *entities.RunServerRequest) (map[string]string, []sessionsettings.ManagedFile) {
	env := make(map[string]string)
	if m.settingsRepo == nil || req == nil {
		return env, nil
	}
	settingsName := req.UserID
	if req.SettingsTeamID != "" {
		settingsName = req.SettingsTeamID
	} else if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		settingsName = req.TeamID
	}
	if settingsName == "" {
		return env, nil
	}
	settings, err := m.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return env, nil
	}
	files := make([]sessionsettings.ManagedFile, 0)
	seenPaths := make(map[string]bool)
	selected := make(map[string]struct{}, len(req.ProfileSecretIDs))
	for _, id := range req.ProfileSecretIDs {
		selected[id] = struct{}{}
	}
	for _, secret := range settings.SecretSettings() {
		if len(selected) > 0 {
			if _, ok := selected[secret.ID]; !ok {
				continue
			}
		}
		for _, projection := range secret.Projections {
			value, ok := secret.Values[projection.Key]
			if !ok {
				continue
			}
			switch projection.Type {
			case "env":
				if _, exists := env[projection.EnvName]; !exists {
					env[projection.EnvName] = value
				}
			case "file":
				clean := filepath.Clean(projection.Path)
				if seenPaths[clean] {
					continue
				}
				seenPaths[clean] = true
				permissions := projection.Permissions
				if permissions == "" {
					permissions = "0600"
				}
				files = append(files, sessionsettings.ManagedFile{Path: clean, Content: value, Permissions: permissions})
			}
		}
	}
	return env, files
}

// createSessionSettingsSecretFromSettings creates the unified session settings Secret
// from a pre-built SessionSettings struct.
// This Secret is used by agent-provisioner for auto-provisioning on Pod restart.
