package provisioner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

const (
// webhookPayloadPath is the well-known path where the webhook payload JSON
// is exposed inside the session container.
// For non-stock sessions this file is provided by a read-only Kubernetes
// Secret volume mount.  For stock sessions (pre-warmed pods) the volume is
// absent; the provisioner writes the file itself from SessionSettings so
// that both paths behave identically.
)

var errSessionStateBackendUnavailable = errors.New("session persistence backend is unavailable")

var runtimeHome = provisionerHome()
var claudeMDPath = filepath.Join(runtimeHome, ".claude", "CLAUDE.md")
var workdirRepoPath = envPath("AGENTAPI_REPO_DIR", filepath.Join(runtimeHome, "workdir", "repo"))
var webhookPayloadPath = nativeRuntimePath("webhook", "payload.json", "/opt/webhook/payload.json")
var codexRequirementsPath = nativeRuntimePath("codex", "requirements.toml", "/etc/codex/requirements.toml")
var acpHistoryPath = nativeRuntimePath("acp-posts", "history.jsonl", "/opt/acp-posts/history.jsonl")
var piAgentInstructionsPath = filepath.Join(runtimeHome, ".pi", "agent", "AGENTS.md")
var piSettingsPath = filepath.Join(runtimeHome, ".pi", "agent", "settings.json")
var antigravityAgentInstructionsPath = filepath.Join(runtimeHome, ".gemini", "AGENTS.md")
var antigravitySettingsPath = filepath.Join(runtimeHome, ".gemini", "antigravity-cli", "settings.json")
var antigravityMCPConfigPath = filepath.Join(runtimeHome, ".gemini", "config", "mcp_config.json")

const sessionRestoreMarkerPrefix = "agentapi-session-restore-complete-"
const sessionProvisionAbortedMarkerPrefix = "agentapi-session-provision-aborted-"

const mountedSettingsSanitizeTimeout = 2 * time.Minute

func sessionRestoreMarkerPath(sessionID string) string {
	return filepath.Join(os.TempDir(), sessionRestoreMarkerPrefix+sessionID)
}

func sessionProvisionAbortedMarkerPath(sessionID string) string {
	return filepath.Join(os.TempDir(), sessionProvisionAbortedMarkerPrefix+sessionID)
}

func workspaceRestoredMarkerPath() string {
	return filepath.Join(filepath.Dir(workdirRepoPath), ".agentapi", "workspace-restored")
}

func workspaceAlreadyRestored(sourceID string) bool {
	if data, err := os.ReadFile(workspaceRestoredMarkerPath()); err == nil && strings.TrimSpace(string(data)) == sourceID {
		return true
	}
	volumePath := strings.TrimSpace(os.Getenv("AGENTAPI_SESSION_STATE_VOLUME_PATH"))
	if volumePath == "" {
		return false
	}
	info, err := os.Stat(volumePath)
	return err == nil && info.Mode().IsRegular()
}

func recordWorkspaceRestored(sourceID string) error {
	path := workspaceRestoredMarkerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sourceID), 0o600)
}

func scheduleSessionSuspendFromReady(ctx context.Context) error {
	proxy := sessionControlProxyURL()
	token := os.Getenv("SESSION_CONTROL_TOKEN")
	sessionID := sessionControlSessionID()
	if proxy == "" || token == "" || sessionID == "" {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, proxy+"/internal/session-state/"+url.PathEscape(sessionID)+"/suspend", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("schedule session suspend returned HTTP %d", resp.StatusCode)
	}
	return nil
}

const (
	containerJavaScriptRuntime  = "/usr/local/bin/node"
	containerCodexACPEntrypoint = "/home/agentapi/.npm-global/bin/codex-acp"
)

func envPath(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func provisionerHome() string {
	if strings.TrimSpace(os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT")) != "" {
		return envPath("HOME", "/home/agentapi")
	}
	return "/home/agentapi"
}

func nativeRuntimePath(parts ...string) string {
	fallback := parts[len(parts)-1]
	parts = parts[:len(parts)-1]
	if root := strings.TrimSpace(os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT")); root != "" {
		pathParts := append([]string{root, "runtime"}, parts...)
		return filepath.Join(pathParts...)
	}
	return fallback
}

func normalizeNativeSettings(settings *sessionsettings.SessionSettings) {
	root := strings.TrimSpace(os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT"))
	if root == "" || settings == nil {
		return
	}
	rewrite := func(path string) string {
		if path == "/home/agentapi" {
			return runtimeHome
		}
		if strings.HasPrefix(path, "/home/agentapi/") {
			return filepath.Join(runtimeHome, strings.TrimPrefix(path, "/home/agentapi/"))
		}
		return path
	}
	if settings.Repository != nil {
		settings.Repository.CloneDir = rewrite(settings.Repository.CloneDir)
		if settings.Repository.CloneDir == "" {
			settings.Repository.CloneDir = workdirRepoPath
		}
	}
	for i := range settings.Repositories {
		settings.Repositories[i].CloneDir = rewrite(settings.Repositories[i].CloneDir)
	}
	for i := range settings.Files {
		settings.Files[i].Path = rewrite(settings.Files[i].Path)
	}
	for i, path := range settings.RemoveFiles {
		settings.RemoveFiles[i] = rewrite(path)
	}
	for i, path := range settings.UnsyncedFilePaths {
		settings.UnsyncedFilePaths[i] = rewrite(path)
	}
	for i := range settings.Storages {
		settings.Storages[i].MountPath = rewrite(settings.Storages[i].MountPath)
	}
	if settings.Env == nil {
		settings.Env = make(map[string]string)
	}
	settings.Env["HOME"] = runtimeHome
	settings.Env["AGENTAPI_WORKDIR"] = envPath("AGENTAPI_WORKDIR", filepath.Dir(workdirRepoPath))
	settings.Env["AGENTAPI_CLONE_DIR"] = workdirRepoPath
	settings.Env["AGENTAPI_PORT"] = envPath("AGENTAPI_PORT", "8080")
	if settings.Session.AgentType == "pi" {
		settings.Env["PI_ACP_PI_COMMAND"] = "pi"
	}
	if binaryPath := strings.TrimSpace(os.Getenv("CCPLANT_BINARY_PATH")); binaryPath != "" {
		settings.Env["CCPLANT_BINARY_PATH"] = binaryPath
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if strings.Contains(settings.Env[key], "127.0.0.1:18081") {
			delete(settings.Env, key)
		}
	}
	// These paths are generated by the Kubernetes SCIA sidecar and do not exist
	// on a native host. Leaving one behind breaks Codex/Node TLS initialization.
	for _, key := range []string{
		"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "NODE_EXTRA_CA_CERTS",
	} {
		value := settings.Env[key]
		if strings.HasPrefix(value, "/etc/scia/") || strings.HasPrefix(value, "/tmp/scia-") {
			delete(settings.Env, key)
		}
	}
	delete(settings.Env, "AGENTAPI_SCIA_PROXY_URL")
	delete(settings.Env, "AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED")
	settings.OtelCollector = nil
	settings.Sandbox = nil
}

// runProvision executes the full provisioning sequence and then supervises
// the agentapi subprocess.
//
// Sequence:
//  1. Run sessionsettings.SetupSettings() from the received in-memory settings
//  2. Keep the session environment in memory
//  4. Fetch memory from the proxy and inject into CLAUDE.md
//  5. cd into the cloned repo if present
//  6. Start agentapi (or an ACP bridge) as a subprocess
//  7. Wait for agentapi to become ready
//  8. Send the initial message if specified in settings
//  9. Set status to "ready"; supervise the subprocess
