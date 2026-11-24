package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	domainservices "github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	infraservices "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// LabelSettings is the label key for settings resources
	LabelSettings = "agentapi.proxy/settings"
	// LabelSettingsName is the label key for settings name
	LabelSettingsName = "agentapi.proxy/settings-name"
	// SecretKeySettings is the key in the Secret data for settings JSON
	SecretKeySettings = "settings.json"
	// SettingsSecretPrefix is the prefix for settings Secret names
	SettingsSecretPrefix = "agentapi-settings-"
)

// encryptedEnvVarJSON is the JSON representation of a single encrypted env var value.
// It embeds the encryption metadata needed to select the correct decryptor.
type encryptedEnvVarJSON struct {
	EncryptedValue string    `json:"v"`
	Algorithm      string    `json:"alg"`
	KeyID          string    `json:"kid"`
	EncryptedAt    time.Time `json:"at"`
	Version        string    `json:"ver,omitempty"`
}

type secretSettingJSON struct {
	ID              string                         `json:"id"`
	Name            string                         `json:"name"`
	Values          map[string]string              `json:"values,omitempty"`
	EncryptedValues map[string]encryptedEnvVarJSON `json:"encrypted_values,omitempty"`
	Projections     []entities.SecretProjection    `json:"projections,omitempty"`
	Version         int64                          `json:"version"`
	CreatedAt       time.Time                      `json:"created_at"`
	UpdatedAt       time.Time                      `json:"updated_at"`
}

// settingsJSON is the JSON representation of settings stored in Secret
type settingsJSON struct {
	CodexConnection         *connectionJSON                        `json:"codex_connection,omitempty"`
	ClaudeConnection        *connectionJSON                        `json:"claude_connection,omitempty"`
	Name                    string                                 `json:"name"`
	Bedrock                 *bedrockJSON                           `json:"bedrock,omitempty"`
	MCPServers              map[string]*mcpServerJSON              `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*marketplaceJSON            `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken    string                                 `json:"claude_code_oauth_token,omitempty"`
	AuthMode                string                                 `json:"auth_mode,omitempty"`
	EnabledPlugins          []string                               `json:"enabled_plugins,omitempty"`    // plugin@marketplace format
	Skills                  []string                               `json:"skills,omitempty"`             // skills.sh package sources
	EnvVars                 map[string]string                      `json:"env_vars,omitempty"`           // plain env vars (legacy / noop)
	EncryptedEnvVars        map[string]encryptedEnvVarJSON         `json:"encrypted_env_vars,omitempty"` // encrypted env vars
	PreferredTeamID         string                                 `json:"preferred_team_id,omitempty"`  // "org/team-slug" format
	GitHubAppInstallationID string                                 `json:"github_app_installation_id,omitempty"`
	GitHubRepositories      []string                               `json:"github_repositories,omitempty"`
	SlackUserID             string                                 `json:"slack_user_id,omitempty"`             // Slack DM notification user ID
	NotificationChannels    []string                               `json:"notification_channels,omitempty"`     // Active notification channels
	ExternalSessionManagers []entities.ExternalSessionManagerEntry `json:"external_session_managers,omitempty"` // Registered external session managers
	DefaultSessionProfileID string                                 `json:"default_session_profile_id,omitempty"`
	TeamProfileIDs          []string                               `json:"enabled_team_session_profile_ids,omitempty"`
	DefaultAgentType        string                                 `json:"default_agent_type,omitempty"`
	AutoSuspend             *entities.AutoSuspendSettings          `json:"auto_suspend,omitempty"`
	SecretSettings          []secretSettingJSON                    `json:"secret_settings,omitempty"`
	CreatedAt               time.Time                              `json:"created_at"`
	UpdatedAt               time.Time                              `json:"updated_at"`
}

// bedrockJSON is the JSON representation of Bedrock settings
type bedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// mcpServerJSON is the JSON representation of a single MCP server
type mcpServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// marketplaceJSON is the JSON representation of a single marketplace
type marketplaceJSON struct {
	URL string `json:"url"`
}

// KubernetesSettingsRepository implements SettingsRepository using Kubernetes Secrets
type KubernetesSettingsRepository struct {
	client             kubernetes.Interface
	namespace          string
	encryptionRegistry *infraservices.EncryptionServiceRegistry // optional; nil = store env_vars as plain text
}

// NewKubernetesSettingsRepository creates a new KubernetesSettingsRepository.
// An optional EncryptionServiceRegistry may be passed to enable at-rest encryption of
// env_vars.  If nil (or if the primary algorithm is "noop"), env_vars are stored as
// plain text in the legacy env_vars field.
func NewKubernetesSettingsRepository(client kubernetes.Interface, namespace string, registry ...*infraservices.EncryptionServiceRegistry) *KubernetesSettingsRepository {
	var reg *infraservices.EncryptionServiceRegistry
	if len(registry) > 0 {
		reg = registry[0]
	}
	return &KubernetesSettingsRepository{
		client:             client,
		namespace:          namespace,
		encryptionRegistry: reg,
	}
}

// Save persists settings (creates or updates)
func (r *KubernetesSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}

	secretName := r.secretName(settings.Name())
	labelValue := sanitizeLabelValue(settings.Name())

	data, err := r.toJSON(ctx, settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelSettings:     "true",
				LabelSettingsName: labelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySettings: data,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Preserve the current resource version for optimistic concurrency.
			// This is required by Kubernetes and by KV-store adapters such as
			// libSQL, which map ResourceVersion to their record version.
			existing, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing settings secret: %w", getErr)
			}
			secret.ResourceVersion = existing.ResourceVersion
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update settings secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create settings secret: %w", err)
	}

	return nil
}

// FindByName retrieves settings by name
func (r *KubernetesSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	secretName := r.secretName(name)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("settings not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get settings secret: %w", err)
	}

	settings, err := r.fromSecret(ctx, secret)
	if err != nil {
		return nil, err
	}

	// Always ensure the name matches the requested name.
	// The stored JSON may have an empty name (legacy entries), so we set it here.
	if settings.Name() == "" {
		settings.SetName(name)
	}

	return settings, nil
}

// Delete removes settings by name
func (r *KubernetesSettingsRepository) Delete(ctx context.Context, name string) error {
	secretName := r.secretName(name)

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("settings not found: %s", name)
		}
		return fmt.Errorf("failed to delete settings secret: %w", err)
	}

	return nil
}

// Exists checks if settings exist for the given name
func (r *KubernetesSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	secretName := r.secretName(name)

	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check settings existence: %w", err)
	}

	return true, nil
}

// List retrieves all settings
func (r *KubernetesSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelSettings),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list settings secrets: %w", err)
	}

	var settingsList []*entities.Settings
	for _, secret := range secrets.Items {
		settings, err := r.fromSecret(ctx, &secret)
		if err != nil {
			// Skip invalid settings
			continue
		}
		settingsList = append(settingsList, settings)
	}

	return settingsList, nil
}

// secretName returns the Secret name for the given settings name
func (r *KubernetesSettingsRepository) secretName(name string) string {
	return SettingsSecretPrefix + sanitizeSecretName(name)
}

// encryptionSvc returns the primary encryption service, or nil if no registry is configured.
func (r *KubernetesSettingsRepository) encryptionSvc() domainservices.EncryptionService {
	if r.encryptionRegistry == nil {
		return nil
	}
	return r.encryptionRegistry.GetForEncryption()
}

// decryptionSvc returns the appropriate decryption service for the given metadata, or nil if no registry is configured.
func (r *KubernetesSettingsRepository) decryptionSvc(metadata domainservices.EncryptionMetadata) domainservices.EncryptionService {
	if r.encryptionRegistry == nil {
		return nil
	}
	return r.encryptionRegistry.GetForDecryption(metadata)
}

// toJSON converts Settings entity to JSON bytes, encrypting env_vars when a non-noop
// EncryptionServiceRegistry is configured.
func (r *KubernetesSettingsRepository) toJSON(ctx context.Context, settings *entities.Settings) ([]byte, error) {
	sj := &settingsJSON{
		Name:                 settings.Name(),
		ClaudeCodeOAuthToken: settings.ClaudeCodeOAuthToken(),
		AuthMode:             string(settings.AuthMode()),
		CreatedAt:            settings.CreatedAt(),
		UpdatedAt:            settings.UpdatedAt(),
	}

	var connectionErr error
	sj.CodexConnection, connectionErr = r.encodeConnection(ctx, settings.CodexConnection())
	if connectionErr != nil {
		return nil, connectionErr
	}
	sj.ClaudeConnection, connectionErr = r.encodeConnection(ctx, settings.ClaudeConnection())
	if connectionErr != nil {
		return nil, connectionErr
	}
	if bedrock := settings.Bedrock(); bedrock != nil {
		sj.Bedrock = &bedrockJSON{
			Enabled:         bedrock.Enabled(),
			Model:           bedrock.Model(),
			AccessKeyID:     bedrock.AccessKeyID(),
			SecretAccessKey: bedrock.SecretAccessKey(),
			RoleARN:         bedrock.RoleARN(),
			Profile:         bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		sj.MCPServers = make(map[string]*mcpServerJSON)
		for name, server := range mcpServers.Servers() {
			sj.MCPServers[name] = &mcpServerJSON{
				Type:    server.Type(),
				URL:     server.URL(),
				Command: server.Command(),
				Args:    server.Args(),
				Env:     server.Env(),
				Headers: server.Headers(),
			}
		}
	}

	if marketplaces := settings.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		sj.Marketplaces = make(map[string]*marketplaceJSON)
		for name, marketplace := range marketplaces.Marketplaces() {
			sj.Marketplaces[name] = &marketplaceJSON{
				URL: marketplace.URL(),
			}
		}
	}

	if plugins := settings.EnabledPlugins(); len(plugins) > 0 {
		sj.EnabledPlugins = plugins
	}
	if skills := settings.Skills(); len(skills) > 0 {
		sj.Skills = skills
	}

	if envVars := settings.EnvVars(); len(envVars) > 0 {
		if enc := r.encryptionSvc(); enc != nil && enc.Algorithm() != "noop" {
			sj.EncryptedEnvVars = make(map[string]encryptedEnvVarJSON, len(envVars))
			for k, v := range envVars {
				encrypted, err := enc.Encrypt(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt env var %q: %w", k, err)
				}
				sj.EncryptedEnvVars[k] = encryptedEnvVarJSON{
					EncryptedValue: encrypted.EncryptedValue,
					Algorithm:      encrypted.Metadata.Algorithm,
					KeyID:          encrypted.Metadata.KeyID,
					EncryptedAt:    encrypted.Metadata.EncryptedAt,
					Version:        encrypted.Metadata.Version,
				}
			}
		} else {
			sj.EnvVars = envVars
		}
	}

	if preferredTeamID := settings.PreferredTeamID(); preferredTeamID != "" {
		sj.PreferredTeamID = preferredTeamID
	}
	if installationID := settings.GitHubAppInstallationID(); installationID != "" {
		sj.GitHubAppInstallationID = installationID
	}
	sj.GitHubRepositories = settings.GitHubRepositories()

	if slackUserID := settings.SlackUserID(); slackUserID != "" {
		sj.SlackUserID = slackUserID
	}

	if channels := settings.NotificationChannels(); len(channels) > 0 {
		sj.NotificationChannels = channels
	}

	if managers := settings.ExternalSessionManagers(); len(managers) > 0 {
		sj.ExternalSessionManagers = managers
	}

	if id := settings.DefaultSessionProfileID(); id != "" {
		sj.DefaultSessionProfileID = id
	}
	sj.TeamProfileIDs = settings.EnabledTeamSessionProfileIDs()
	if agentType := settings.DefaultAgentType(); agentType != "" {
		sj.DefaultAgentType = agentType
	}
	sj.AutoSuspend = settings.AutoSuspend()
	for _, secretSetting := range settings.SecretSettings() {
		stored := secretSettingJSON{
			ID: secretSetting.ID, Name: secretSetting.Name,
			Projections: secretSetting.Projections, Version: secretSetting.Version,
			CreatedAt: secretSetting.CreatedAt, UpdatedAt: secretSetting.UpdatedAt,
		}
		if enc := r.encryptionSvc(); enc != nil && enc.Algorithm() != "noop" {
			stored.EncryptedValues = make(map[string]encryptedEnvVarJSON, len(secretSetting.Values))
			for key, value := range secretSetting.Values {
				encrypted, err := enc.Encrypt(ctx, value)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt secret setting %q key %q: %w", secretSetting.ID, key, err)
				}
				stored.EncryptedValues[key] = encryptedEnvVarJSON{
					EncryptedValue: encrypted.EncryptedValue, Algorithm: encrypted.Metadata.Algorithm,
					KeyID: encrypted.Metadata.KeyID, EncryptedAt: encrypted.Metadata.EncryptedAt,
					Version: encrypted.Metadata.Version,
				}
			}
		} else {
			stored.Values = secretSetting.Values
		}
		sj.SecretSettings = append(sj.SecretSettings, stored)
	}

	return json.Marshal(sj)
}

// fromSecret converts a Kubernetes Secret to Settings entity, decrypting env_vars as needed.
