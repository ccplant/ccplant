package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settingspatch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (m *KubernetesSessionManager) createSessionSettingsSecretFromSettings(
	ctx context.Context,
	session *KubernetesSession,
	req *entities.RunServerRequest,
	settings *sessionsettings.SessionSettings,
) error {
	yamlData, err := marshalMountedSessionSettings(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal session settings to YAML: %w", err)
	}

	secretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            secretName,
			Namespace:       m.namespace,
			OwnerReferences: m.sessionServiceOwnerReferences(ctx, session.id),
			Labels: map[string]string{
				"agentapi.proxy/session-id": session.id,
				"agentapi.proxy/user-id":    sanitizeLabelValue(req.UserID),
				"agentapi.proxy/resource":   "session-settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"settings.yaml": yamlData,
		},
	}

	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create session settings secret: %w", err)
	}

	log.Printf("[K8S_SESSION] Created session settings Secret %s for session %s", secretName, session.id)
	return nil
}

// marshalMountedSessionSettings removes bootstrap-only credentials from the
// settings file visible to the agent. The provisioner receives the complete
// settings through the authenticated claim response and keeps the key only in
// memory for its local CLI broker.
func marshalMountedSessionSettings(settings *sessionsettings.SessionSettings) ([]byte, error) {
	persisted := *settings
	persisted.Env = maps.Clone(settings.Env)
	delete(persisted.Env, "AGENTAPI_KEY")
	return sessionsettings.MarshalYAML(&persisted)
}

// deleteSessionSettingsSecret deletes the unified session settings Secret.
func (m *KubernetesSessionManager) deleteSessionSettingsSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("agentapi-session-%s-settings", session.id)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete session settings secret: %w", err)
	}
	return nil
}

// deleteOneshotSettingsSecret deletes the oneshot settings Secret.
// This fixes the existing bug where oneshot-settings secrets were not being cleaned up.
func (m *KubernetesSessionManager) deleteOneshotSettingsSecret(ctx context.Context, session *KubernetesSession) error {
	secretName := fmt.Sprintf("%s-oneshot-settings", session.ServiceName())
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete oneshot settings secret: %w", err)
	}
	return nil
}

// readSettingsPatch reads the settings.json from an agentapi-settings-* Secret
// and returns it as a SettingsPatch. Returns nil if the secret does not exist or cannot be parsed.
func (m *KubernetesSessionManager) readSettingsPatch(ctx context.Context, secretName string) *settingspatch.SettingsPatch {
	client, namespace := m.client, m.namespace
	if m.settingsSecretClient != nil {
		client, namespace = m.settingsSecretClient, m.settingsSecretNamespace
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("[K8S_SESSION] Warning: failed to read settings secret %s: %v", secretName, err)
		}
		return nil
	}
	data, ok := secret.Data["settings.json"]
	if !ok {
		return nil
	}
	patch, err := settingspatch.FromJSON(data)
	if err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to parse settings.json from secret %s: %v", secretName, err)
		return nil
	}
	return &patch
}

// readLegacyBaseMCPPatch reads the organization-wide MCP configuration used
// before MCP servers were folded into agentapi-settings-base. The persistence
// client may be backed by Kubernetes or by the libSQL compatibility adapter.
func (m *KubernetesSessionManager) readLegacyBaseMCPPatch(ctx context.Context, secretName string) *settingspatch.SettingsPatch {
	if secretName == "" {
		return nil
	}
	client, namespace := m.client, m.namespace
	if m.settingsSecretClient != nil {
		client, namespace = m.settingsSecretClient, m.settingsSecretNamespace
	}
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Printf("[K8S_SESSION] Warning: failed to read legacy MCP base secret %s: %v", secretName, err)
		}
		return nil
	}
	data, ok := secret.Data["mcp-servers.json"]
	if !ok {
		return nil
	}
	var legacy struct {
		MCPServers map[string]*settingspatch.MCPServerPatch `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		log.Printf("[K8S_SESSION] Warning: failed to parse mcp-servers.json from secret %s: %v", secretName, err)
		return nil
	}
	if len(legacy.MCPServers) == 0 {
		return nil
	}
	return &settingspatch.SettingsPatch{MCPServers: legacy.MCPServers}
}

// readSettingsPatchByName reads an agentapi settings entry through SettingsRepository.
// The repository is responsible for storage details such as encrypted_env_vars, so
// session startup receives plaintext values before merge/materialize.
func (m *KubernetesSessionManager) readSettingsPatchByName(ctx context.Context, settingsName string) *settingspatch.SettingsPatch {
	if settingsName == "" {
		return nil
	}
	secretName := fmt.Sprintf("agentapi-settings-%s", sanitizeSecretName(settingsName))
	rawPatch := m.readSettingsPatch(ctx, secretName)
	if m.settingsRepo == nil {
		return rawPatch
	}
	settings, err := m.settingsRepo.FindByName(ctx, settingsName)
	if err != nil {
		return rawPatch
	}
	if rawPatch != nil {
		patch := *rawPatch
		if envVars := settings.EnvVars(); len(envVars) > 0 {
			patch.EnvVars = cloneStringMap(envVars)
		}
		return &patch
	}
	patch := settingsToPatch(settings)
	return &patch
}

func settingsToPatch(settings *entities.Settings) settingspatch.SettingsPatch {
	patch := settingspatch.SettingsPatch{
		AuthMode:        string(settings.AuthMode()),
		OAuthToken:      settings.ClaudeCodeOAuthToken(),
		EnvVars:         cloneStringMap(settings.EnvVars()),
		EnabledPlugins:  append([]string(nil), settings.EnabledPlugins()...),
		Skills:          append([]string(nil), settings.Skills()...),
		PreferredTeamID: settings.PreferredTeamID(),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		patch.Bedrock = &settingspatch.BedrockPatch{
			Model:           bedrock.Model(),
			AccessKeyID:     bedrock.AccessKeyID(),
			SecretAccessKey: bedrock.SecretAccessKey(),
			RoleARN:         bedrock.RoleARN(),
			Profile:         bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		patch.MCPServers = make(map[string]*settingspatch.MCPServerPatch, len(mcpServers.Servers()))
		for name, server := range mcpServers.Servers() {
			patch.MCPServers[name] = &settingspatch.MCPServerPatch{
				Type:    server.Type(),
				URL:     server.URL(),
				Command: server.Command(),
				Args:    append([]string(nil), server.Args()...),
				Env:     cloneStringMap(server.Env()),
				Headers: cloneStringMap(server.Headers()),
			}
		}
	}

	if marketplaces := settings.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		patch.Marketplaces = make(map[string]*settingspatch.MarketplacePatch, len(marketplaces.Marketplaces()))
		for name, marketplace := range marketplaces.Marketplaces() {
			patch.Marketplaces[name] = &settingspatch.MarketplacePatch{URL: marketplace.URL()}
		}
	}

	return patch
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// resolveSettings reads settings patches from the relevant Kubernetes Secrets
// (base → team[] → user → oneshot) and returns materialized session configuration.
//
// This is the single entry point for all settings merging. It replaces the previous
// dual-path approach (readAgentapiSettingsSecret + expandSettingsToEnv for env vars,
// and mergeSettingsAndMCP for Claude settings JSON).
//
// When the user has preferred_team_id set in their personal settings, only that team's
// settings are applied (instead of all teams). This allows users to explicitly choose
// which team's settings (Bedrock, MCP servers, env vars, etc.) to use.
