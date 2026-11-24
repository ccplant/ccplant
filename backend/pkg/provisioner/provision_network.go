package provisioner

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func writeWebhookPayloadFile(payload string) {
	writeWebhookPayloadFileToPath(webhookPayloadPath, payload)
}

// writeWebhookPayloadFileToPath is the path-parameterised implementation
// shared by writeWebhookPayloadFile and unit tests.
func writeWebhookPayloadFileToPath(path, payload string) {
	if _, err := os.Stat(path); err == nil {
		// File already present (non-stock session: Secret volume mount).
		log.Printf("[PROVISIONER] Webhook payload file already exists at %s, skipping write", path)
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to create webhook payload dir %s: %v", dir, err)
		return
	}

	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write webhook payload file %s: %v", path, err)
		return
	}

	log.Printf("[PROVISIONER] Wrote webhook payload to %s (%d bytes)", path, len(payload))
}

// mergeEnv takes the current os.Environ() slice and overlays envMap entries.
// The envMap values take precedence (session-specific overrides).
func mergeEnv(base []string, overlay map[string]string) []string {
	// Build a map from base env (last occurrence wins if duplicated).
	merged := make(map[string]string, len(base)+len(overlay))
	for _, kv := range base {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		merged[kv[:idx]] = kv[idx+1:]
	}
	for k, v := range overlay {
		merged[k] = v
	}

	result := make([]string, 0, len(merged))
	for k, v := range merged {
		result = append(result, k+"="+v)
	}
	sort.Strings(result)
	return result
}

func cloneEnvironment(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// sessionProcessEnvironment builds the environment for every process that can
// execute session-controlled code. The API key is a provisioner bootstrap
// credential and must remain available only to the in-process CLI broker.
func sessionProcessEnvironment(base []string, unset []string, overlay map[string]string) []string {
	blocked := append([]string(nil), unset...)
	blocked = append(blocked, "AGENTAPI_KEY")

	sanitizedOverlay := cloneEnvironment(overlay)
	delete(sanitizedOverlay, "AGENTAPI_KEY")
	return mergeEnv(withoutEnvironment(base, blocked), sanitizedOverlay)
}

// configureNetworkFilterPolicy calls POST /policy on the nfa control server to
// apply the concrete sandbox policy from SessionSettings. This is important for
// stock sessions: their sandbox sidecar starts before the user's policy/count
// mode settings are known.
func configureNetworkFilterPolicy(sandbox *sessionsettings.SandboxConfig) {
	if sandbox == nil || !sandbox.Enabled {
		return
	}
	payload := map[string]interface{}{
		"allowed":    sessionsettings.SandboxAllowedDomains(sandbox.AllowedDomains, sandbox.DeniedDomains),
		"denied":     sandbox.DeniedDomains,
		"count_mode": sandbox.CountMode,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[PROVISIONER] network-filter configure-policy marshal: %v", err)
		return
	}
	client, base := networkFilterControlClient()
	resp, err := client.Post(base+"/policy", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Printf("[PROVISIONER] network-filter configure-policy: %v (no sandbox sidecar?)", err)
		return
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	log.Printf("[PROVISIONER] network-filter policy configured (status %s, count_mode=%t)", resp.Status, sandbox.CountMode)
}

// enableNetworkFilterPolicy calls POST /enable-policy on the network-filter
// control server (localhost:3129) so that the sandbox policy takes effect after
// the agent has fully started. The call is best-effort: if there is no sandbox
// sidecar the connection is refused and the error is silently ignored.
func enableNetworkFilterPolicy() {
	client, base := networkFilterControlClient()
	resp, err := client.Post(base+"/enable-policy", "", nil)
	if err != nil {
		log.Printf("[PROVISIONER] network-filter enable-policy: %v (no sandbox sidecar?)", err)
		return
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	log.Printf("[PROVISIONER] network-filter policy enabled (status %s)", resp.Status)
}

func networkFilterControlClient() (*http.Client, string) {
	if socketPath := strings.TrimSpace(os.Getenv("NETWORK_FILTER_CONTROL_SOCKET")); socketPath != "" {
		return unixSocketHTTPClient(socketPath), "http://unix"
	}
	return http.DefaultClient, "http://127.0.0.1:3129"
}
