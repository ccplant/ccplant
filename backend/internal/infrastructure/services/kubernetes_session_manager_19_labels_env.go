package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// This allows filtering at the API level for better performance
func (m *KubernetesSessionManager) buildLabelSelector(filter entities.SessionFilter) string {
	// Base selector for agentapi sessions.
	// "!agentapi.proxy/stock" excludes Services that are still in stock state
	// (stock=true: unclaimed, stock=claiming: being adopted).  This prevents
	// other proxy replicas from restoring a stock/claiming Service with an empty
	// user-id during the adoption window, which would cause 403 errors and
	// sessions appearing/disappearing across replicas.
	selector := "app.kubernetes.io/managed-by=agentapi-proxy,app.kubernetes.io/name=agentapi-session,!agentapi.proxy/stock"

	// Add UserID filter
	if filter.UserID != "" {
		selector += ",agentapi.proxy/user-id=" + sanitizeLabelValue(filter.UserID)
	}

	// Add Scope filter (only for team scope to maintain backward compatibility)
	// Note: scope=user is not added to LabelSelector because old sessions may not have
	// the scope label set. These sessions should be treated as user-scoped by default.
	// Go-level filtering handles scope=user cases properly via session.Scope() method.
	if filter.Scope == entities.ScopeTeam {
		selector += ",agentapi.proxy/scope=" + string(filter.Scope)
	}

	// Add TeamID filter using sha256 hash for consistent matching
	if filter.TeamID != "" {
		selector += ",agentapi.proxy/team-id-hash=" + hashTeamID(filter.TeamID)
	}

	for key, value := range filter.Tags {
		selector += ",agentapi.proxy/tag-" + sanitizeLabelKey(key) + "=" + sanitizeLabelValue(value)
	}

	return m.managerScopedSelector(selector)
}

// buildLabels creates standard labels for Kubernetes resources
func (m *KubernetesSessionManager) buildLabels(session *KubernetesSession) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/instance":   session.id,
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    session.id,
		"agentapi.proxy/user-id":       sanitizeLabelValue(session.Request().UserID),
	}
	if m.config != nil && m.config.SessionManager.ID != "" {
		labels["agentapi.proxy/session-manager-id"] = sanitizeLabelValue(m.config.SessionManager.ID)
	}

	// Add scope and team_id labels for filtering
	// Always set scope label (default to "user" if not specified)
	scope := session.Request().Scope
	if scope == "" {
		scope = entities.ScopeUser
	}
	labels["agentapi.proxy/scope"] = string(scope)
	if session.Request().TeamID != "" {
		// Use sha256 hash for team-id label to avoid sanitization issues with "/" in team IDs
		// The original team_id is stored in annotations for restoration
		labels["agentapi.proxy/team-id-hash"] = hashTeamID(session.Request().TeamID)
	}

	// Add tags as labels (sanitized for Kubernetes)
	for k, v := range session.Request().Tags {
		labelKey := fmt.Sprintf("agentapi.proxy/tag-%s", sanitizeLabelKey(k))
		labels[labelKey] = sanitizeLabelValue(v)
	}

	if session.isStock {
		labels["agentapi.proxy/stock"] = "true"
	}
	req := session.Request()
	// Sandbox (network filter) is always enabled
	labels["agentapi.proxy/capability-sandbox"] = "true"
	labels["agentapi.proxy/capability-dind"] = fmt.Sprintf("%t", req.Docker != nil && req.Docker.Enabled)
	if req.AgentType != "" {
		labels["agentapi.proxy/agent-type"] = sanitizeLabelValue(req.AgentType)
	}

	return labels
}

// managerScopedSelector confines inventory and cleanup operations to the
// session manager that created the resources. Managers without an ID retain
// the legacy namespace-wide behavior used by local, non-runner deployments.
func (m *KubernetesSessionManager) managerScopedSelector(selector string) string {
	if m.config == nil || m.config.SessionManager.ID == "" {
		return selector
	}
	return selector + ",agentapi.proxy/session-manager-id=" + sanitizeLabelValue(m.config.SessionManager.ID)
}

// buildEnvVars creates environment variables for the session pod
func (m *KubernetesSessionManager) buildEnvVars(session *KubernetesSession, req *entities.RunServerRequest) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: proxybinary.EnvName, Value: m.sessionBinaryPath()},
		{Name: "AGENTAPI_PORT", Value: fmt.Sprintf("%d", m.k8sConfig.BasePort)},
		{Name: "AGENTAPI_SESSION_ID", Value: session.id},
		// Provision settings replace AGENTAPI_SESSION_ID with the public parent
		// ID after a stock runner is claimed. Keep the local workload ID stable
		// for session-scoped calls back to this manager.
		{Name: "AGENTAPI_ALLOCATED_SESSION_ID", Value: session.id},
		{Name: "AGENTAPI_USER_ID", Value: req.UserID},
		{Name: "HOME", Value: "/home/agentapi"},
		// GitHub App PEM path (file is written by setup directly to container FS)
		{Name: "GITHUB_APP_PEM_PATH", Value: "/tmp/github-app/app.pem"},
	}

	// Add Claude Code telemetry configuration
	if m.k8sConfig.OtelCollectorEnabled {
		envVars = append(envVars,
			corev1.EnvVar{Name: "CLAUDE_CODE_ENABLE_TELEMETRY", Value: "1"},
			corev1.EnvVar{Name: "OTEL_METRICS_EXPORTER", Value: "prometheus"},
		)
	}

	// Add Team ID if in team scope
	if req.Scope == entities.ScopeTeam && req.TeamID != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_TEAM_ID", Value: req.TeamID})
	}

	// Add Agent Type if specified
	if req.AgentType != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_AGENT_TYPE", Value: req.AgentType})
	}

	proxyURL := m.k8sConfig.ProvisionerProxyURL
	if proxyURL == "" {
		proxyURL = fmt.Sprintf("http://control.%s.svc.cluster.local:8080", m.namespace)
	}
	localProxyURL := strings.TrimRight(m.config.SessionManager.LocalURL, "/")
	if localProxyURL == "" {
		localProxyURL = proxyURL
	}
	envVars = append(envVars,
		corev1.EnvVar{Name: "PROVISIONER_PROXY_URL", Value: proxyURL},
		corev1.EnvVar{Name: "PROVISIONER_LOCAL_PROXY_URL", Value: localProxyURL},
		corev1.EnvVar{Name: "SESSION_STATE_PROXY_URL", Value: proxyURL},
		corev1.EnvVar{Name: "PROVISIONER_TOKEN", Value: deriveSessionControlToken(m.k8sConfig.ProvisionerToken, session.id)},
		// Managed-file sync always calls the Session Manager's local control API,
		// including when commands/events are relayed from a remote parent.
		corev1.EnvVar{Name: "SESSION_CONTROL_TOKEN", Value: deriveSessionControlToken(m.k8sConfig.ProvisionerToken, session.id)},
		corev1.EnvVar{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			}},
		},
		corev1.EnvVar{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.namespace",
			}},
		},
	)
	if m.config.SessionPersistence.Backend == "volume" {
		envVars = append(envVars, corev1.EnvVar{Name: "AGENTAPI_SESSION_STATE_VOLUME_PATH", Value: "/home/agentapi/workdir/.agentapi/session-state.tar.zst"})
	}
	// Add CLAUDE_ARGS from request environment or proxy's environment
	claudeArgs := ""
	if req.Environment != nil {
		if v, ok := req.Environment["CLAUDE_ARGS"]; ok {
			claudeArgs = v
		}
	}
	if claudeArgs == "" && req.ProfileEnvironment != nil {
		claudeArgs = req.ProfileEnvironment["CLAUDE_ARGS"]
	}
	if claudeArgs == "" {
		claudeArgs = os.Getenv("CLAUDE_ARGS")
	}
	if claudeArgs != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "CLAUDE_ARGS", Value: claudeArgs})
	}

	// Add repository info if available
	if req.RepoInfo != nil {
		envVars = append(envVars,
			corev1.EnvVar{Name: "AGENTAPI_REPO_FULLNAME", Value: req.RepoInfo.FullName},
			corev1.EnvVar{Name: "AGENTAPI_CLONE_DIR", Value: "/home/agentapi/workdir/repo"},
		)
	}

	// Profile environment overrides team/user envFrom settings. Explicit request
	// values are merged last and remain highest priority.
	sessionEnv := make(map[string]string, len(req.ProfileEnvironment)+len(req.Environment))
	for k, v := range req.ProfileEnvironment {
		sessionEnv[k] = v
	}
	for k, v := range req.Environment {
		sessionEnv[k] = v
	}
	// Add the merged session environment (except CLAUDE_ARGS which is already handled).
	for k, v := range sessionEnv {
		if k != "CLAUDE_ARGS" {
			envVars = append(envVars, corev1.EnvVar{Name: k, Value: v})
		}
	}

	if req.AgentType == "pi" {
		envVars = ensurePiPodEnv(envVars)
	}

	// Add VAPID environment variables for push notifications
	vapidEnvVars := []string{"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "VAPID_CONTACT_EMAIL"}
	for _, envName := range vapidEnvVars {
		if value := os.Getenv(envName); value != "" {
			envVars = append(envVars, corev1.EnvVar{Name: envName, Value: value})
		}
	}

	// Add notification base URL so session pods can construct correct notification URLs
	if value := os.Getenv("NOTIFICATION_BASE_URL"); value != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "NOTIFICATION_BASE_URL", Value: value})
	}

	if m.k8sConfig.CLIImage != "" {
		filtered := make([]corev1.EnvVar, 0, len(envVars))
		for _, env := range envVars {
			if env.Name != proxybinary.EnvName {
				filtered = append(filtered, env)
			}
		}
		envVars = append(filtered, corev1.EnvVar{Name: proxybinary.EnvName, Value: sessionCLIPath})
	}

	// Note: Bedrock settings are now loaded via envFrom from agent-env-{name} Secret
	// which is synced by CredentialsSecretSyncer when settings are updated via API

	return deduplicateEnvVars(envVars)
}

// deduplicateEnvVars preserves first-seen ordering while applying the last
// value for each name. Kubernetes accepts duplicate names, but container
// runtimes resolve them ambiguously and can select the wrong control plane.
func deduplicateEnvVars(envVars []corev1.EnvVar) []corev1.EnvVar {
	result := make([]corev1.EnvVar, 0, len(envVars))
	indexes := make(map[string]int, len(envVars))
	for _, envVar := range envVars {
		if index, ok := indexes[envVar.Name]; ok {
			result[index] = envVar
			continue
		}
		indexes[envVar.Name] = len(result)
		result = append(result, envVar)
	}
	return result
}

const (
	piInstallPreScript = `mkdir -p "$HOME/.pi/agent/npm"
test -f "$HOME/.pi/agent/npm/package.json" || printf '{"private":true,"dependencies":{}}\n' > "$HOME/.pi/agent/npm/package.json"
if [ -d "$HOME/.pi/agent/npm/node_modules/pi-mcp-adapter" ]; then
  echo "Pi extensions already installed, skipping install"
else
  NPM_SHIM_DIR="$(mktemp -d)"
  trap 'rm -rf "$NPM_SHIM_DIR"' EXIT
  cat > "$NPM_SHIM_DIR/npm" <<'EOF'
#!/bin/sh
set -e
prefix=""
packages=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    install) shift ;;
    --prefix) prefix="$2"; shift 2 ;;
    --legacy-peer-deps) shift ;;
    *) packages="$packages $1"; shift ;;
  esac
done
if [ -z "$prefix" ]; then prefix="$PWD"; fi
mkdir -p "$prefix"
test -f "$prefix/package.json" || printf '%s\n' '{"private":true,"dependencies":{}}' > "$prefix/package.json"
exec bun add --cwd "$prefix" $packages
EOF
  chmod +x "$NPM_SHIM_DIR/npm"
  if [ ! -d "$HOME/.pi/agent/npm/node_modules/pi-mcp-adapter" ]; then
    PATH="$NPM_SHIM_DIR:$PATH" pi install npm:pi-mcp-adapter
  fi
  rm -rf "$NPM_SHIM_DIR"
  trap - EXIT
fi`
)

func ensurePiPodEnv(envVars []corev1.EnvVar) []corev1.EnvVar {
	values := make(map[string]string, len(envVars))
	for _, envVar := range envVars {
		if envVar.ValueFrom == nil {
			values[envVar.Name] = envVar.Value
		}
	}

	if strings.TrimSpace(values["PI_ACP_PI_COMMAND"]) == "" {
		envVars = append(envVars, corev1.EnvVar{Name: "PI_ACP_PI_COMMAND", Value: "pi"})
	}

	return envVars
}

func ensurePiSettingsEnv(env map[string]string) {
	if env == nil {
		return
	}
	if strings.TrimSpace(env["PI_ACP_PI_COMMAND"]) == "" {
		env["PI_ACP_PI_COMMAND"] = "pi"
	}
}

// sanitizeLabelKey sanitizes a string to be used as a Kubernetes label key
func sanitizeLabelKey(s string) string {
	// Label keys must be 63 characters or less
	// Must start and end with alphanumeric character
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Trim non-alphanumeric characters from start and end
	sanitized = strings.Trim(sanitized, "-_.")
	return sanitized
}

// hashTeamID creates a sha256 hash of the team ID for use as a Kubernetes label value
// This allows querying by team_id without sanitization issues (e.g., "/" in team IDs)
// The hash is truncated to 63 characters to fit within Kubernetes label value limits
func hashTeamID(teamID string) string {
	if teamID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(teamID))
	hexHash := hex.EncodeToString(hash[:])
	// Truncate to 63 characters (Kubernetes label value limit)
	if len(hexHash) > 63 {
		hexHash = hexHash[:63]
	}
	return hexHash
}

// int64Ptr returns a pointer to an int64
func int64Ptr(i int64) *int64 {
	return &i
}

// boolPtr returns a pointer to a bool
func boolPtr(b bool) *bool {
	return &b
}

// isPVCEnabled returns whether PVC is enabled for session workdir
// Returns true by default if not explicitly set
func (m *KubernetesSessionManager) isPVCEnabled() bool {
	if m.config.SessionPersistence.Backend == "volume" {
		return true
	}
	if m.k8sConfig.PVCEnabled == nil {
		return true // Default to enabled
	}
	return *m.k8sConfig.PVCEnabled
}

// GetClient returns the Kubernetes client (used by subscription secret syncer)
func (m *KubernetesSessionManager) GetClient() kubernetes.Interface {
	return m.client
}
