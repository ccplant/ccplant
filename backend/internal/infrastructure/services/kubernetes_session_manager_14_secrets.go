package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultSlackBotTokenSecretKey = "bot-token"

// deleteWebhookPayloadSecret deletes the webhook payload Secret for a session
// left behind by versions that mounted webhook payloads instead of sending them
// in provision requests.
func (m *KubernetesSessionManager) deleteWebhookPayloadSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-webhook-payload", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete webhook payload secret: %w", err)
	}
	return nil
}

// getInitialMessageFromSecret retrieves the initial message from the session-settings Secret.
// The initial message is stored as the "initial_message" field inside "settings.yaml".
// serviceName follows the pattern "agentapi-session-{id}-svc"; the settings secret is "agentapi-session-{id}-settings".
func (m *KubernetesSessionManager) getInitialMessageFromSecret(ctx context.Context, serviceName string) string {
	settingsSecretName := strings.TrimSuffix(serviceName, "-svc") + "-settings"
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	yamlData, ok := secret.Data["settings.yaml"]
	if !ok {
		return ""
	}
	settings, err := sessionsettings.LoadSettingsFromBytes(yamlData)
	if err != nil {
		return ""
	}
	return settings.InitialMessage
}

// getSessionMetaFromSecret reads the settings secret and returns the SessionMeta
// (including Teams and AgentType) for session restore.
func (m *KubernetesSessionManager) getSessionMetaFromSecret(ctx context.Context, serviceName string) *sessionsettings.SessionMeta {
	settingsSecretName := strings.TrimSuffix(serviceName, "-svc") + "-settings"
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	yamlData, ok := secret.Data["settings.yaml"]
	if !ok {
		return nil
	}
	settings, err := sessionsettings.LoadSettingsFromBytes(yamlData)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to parse settings.yaml from secret %s: %v", settingsSecretName, err)
		return nil
	}
	return &settings.Session
}

// buildCodexHooksJSON converts a Claude Code settings.json hooks map into the
// equivalent Codex hooks.json structure. The hook entry format is identical between
// the two runtimes, so we copy the hooks subtree directly.
func buildCodexHooksJSON(settingsJSON map[string]interface{}) map[string]interface{} {
	if settingsJSON == nil {
		return nil
	}
	hooksMap, ok := settingsJSON["hooks"].(map[string]interface{})
	if !ok || len(hooksMap) == 0 {
		return nil
	}
	return map[string]interface{}{"hooks": hooksMap}
}

// createOneshotSettingsSecret creates a Secret containing settings.json with Stop hook
// This is used when oneshot is enabled to automatically delete the session after stopping
func (m *KubernetesSessionManager) createOneshotSettingsSecret(
	ctx context.Context,
	session *KubernetesSession,
) error {
	secretName := fmt.Sprintf("%s-oneshot-settings", session.ServiceName())

	// Create settings.json with Stop hook
	settingsJSON := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []map[string]interface{}{
				{
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": proxybinary.ShellReference() + " client delete-session --confirm",
						},
					},
				},
			},
		},
	}

	settingsData, err := json.Marshal(settingsJSON)
	if err != nil {
		return fmt.Errorf("failed to marshal oneshot settings: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            secretName,
			Namespace:       m.namespace,
			OwnerReferences: m.sessionServiceOwnerReferences(ctx, session.id),
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(session.Request().UserID),
				"agentapi.proxy/resource":   "oneshot-settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"settings.json": settingsData,
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create oneshot settings secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created oneshot settings Secret %s for session %s", secretName, session.id)
	return nil
}

// resolveSandboxParams returns effective SandboxParams by merging a referenced SandboxPolicy
// (if PolicyID is set) with the session-level overrides in req.Sandbox.
// Policy domains are prepended; session-level domains are appended on top.
func (m *KubernetesSessionManager) resolveSandboxParams(ctx context.Context, req *entities.RunServerRequest) *entities.SandboxParams {
	if req.Sandbox == nil {
		return nil
	}
	effective := &entities.SandboxParams{
		Enabled:        req.Sandbox.Enabled,
		AllowedDomains: req.Sandbox.AllowedDomains,
		DeniedDomains:  req.Sandbox.DeniedDomains,
		CountMode:      req.Sandbox.CountMode,
	}
	if req.Sandbox.PolicyID == "" || m.sandboxPolicyRepo == nil {
		return effective
	}
	policy, err := m.sandboxPolicyRepo.GetByID(ctx, req.Sandbox.PolicyID)
	if err != nil {
		log.Printf("[K8S_SESSION] sandbox policy %s not found, ignoring: %v", req.Sandbox.PolicyID, err)
		return effective
	}
	effective.AllowedDomains = append(policy.AllowedDomains(), effective.AllowedDomains...)
	effective.DeniedDomains = append(policy.DeniedDomains(), effective.DeniedDomains...)
	if policy.CountMode() {
		effective.CountMode = true
	}
	return effective
}

func stockSandboxParams() *entities.SandboxParams {
	return &entities.SandboxParams{Enabled: true, CountMode: true}
}

func applySandboxDefaults(req *entities.RunServerRequest) {
	if req.Sandbox == nil {
		req.Sandbox = &entities.SandboxParams{Enabled: true, CountMode: true}
		return
	}
	req.Sandbox.Enabled = true
	if req.Sandbox.PolicyID == "" && len(req.Sandbox.AllowedDomains) == 0 && len(req.Sandbox.DeniedDomains) == 0 {
		req.Sandbox.CountMode = true
	}
}

func applySandboxPolicyToProvisioner(ctx context.Context, stockSvc *corev1.Service, sandbox *entities.SandboxParams) error {
	body, err := json.Marshal(struct {
		Allowed   []string `json:"allowed,omitempty"`
		Denied    []string `json:"denied,omitempty"`
		CountMode bool     `json:"count_mode,omitempty"`
	}{
		Allowed:   sessionsettings.SandboxAllowedDomains(sandbox.AllowedDomains, sandbox.DeniedDomains),
		Denied:    sandbox.DeniedDomains,
		CountMode: sandbox.CountMode,
	})
	if err != nil {
		return fmt.Errorf("marshal sandbox policy: %w", err)
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/sandbox-policy", stockSvc.Name, stockSvc.Namespace, ProvisionerPort)
	client := &http.Client{Timeout: 2 * time.Second}
	return retrySandboxPolicy(ctx, client, url, body, 60, 500*time.Millisecond)
}

// retrySandboxPolicy tolerates the window between a stock workload becoming
// claimable and its provisioner HTTP server beginning to listen. Container
// startup can legitimately take longer than the previous five-second retry
// window, especially while nodes are under load.
func retrySandboxPolicy(ctx context.Context, client *http.Client, url string, body []byte, attempts int, delay time.Duration) error {
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := postSandboxPolicy(ctx, client, url, body); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

func postSandboxPolicy(ctx context.Context, client *http.Client, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create sandbox policy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post sandbox policy: %w", err)
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("post sandbox policy returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

// buildSandboxContainers returns the init containers (iptables rule generation
// and restore) and sidecar (network-filter proxy) needed for session network sandboxing.
