package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	domainservices "github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	corev1 "k8s.io/api/core/v1"
)

func (r *KubernetesSettingsRepository) fromSecret(ctx context.Context, secret *corev1.Secret) (*entities.Settings, error) {
	data, ok := secret.Data[SecretKeySettings]
	if !ok {
		return nil, fmt.Errorf("secret missing settings data")
	}

	var sj settingsJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Use the name from JSON; fall back to the Secret label if it is empty
	// (which can happen with settings stored before the name field was populated).
	settingsName := sj.Name
	if settingsName == "" {
		settingsName = secret.Labels[LabelSettingsName]
	}
	settings := entities.NewSettings(settingsName)
	settings.SetCreatedAt(sj.CreatedAt)
	settings.SetUpdatedAt(sj.UpdatedAt)
	if len(sj.SecretSettings) > 0 {
		secretSettings := make([]entities.SecretSetting, 0, len(sj.SecretSettings))
		for _, stored := range sj.SecretSettings {
			values := make(map[string]string, len(stored.Values)+len(stored.EncryptedValues))
			for key, value := range stored.Values {
				values[key] = value
			}
			for key, encrypted := range stored.EncryptedValues {
				decSvc := r.decryptionSvc(domainservices.EncryptionMetadata{
					Algorithm: encrypted.Algorithm, KeyID: encrypted.KeyID,
					EncryptedAt: encrypted.EncryptedAt, Version: encrypted.Version,
				})
				if decSvc == nil {
					return nil, fmt.Errorf("no decryption service for secret setting %q key %q", stored.ID, key)
				}
				plaintext, err := decSvc.Decrypt(ctx, &domainservices.EncryptedData{
					EncryptedValue: encrypted.EncryptedValue,
					Metadata: domainservices.EncryptionMetadata{
						Algorithm: encrypted.Algorithm, KeyID: encrypted.KeyID,
						EncryptedAt: encrypted.EncryptedAt, Version: encrypted.Version,
					},
				})
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt secret setting %q key %q: %w", stored.ID, key, err)
				}
				values[key] = plaintext
			}
			secretSettings = append(secretSettings, entities.SecretSetting{
				ID: stored.ID, Name: stored.Name, Values: values, Projections: stored.Projections,
				Version: stored.Version, CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
			})
		}
		settings.SetSecretSettings(secretSettings)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(sj.Bedrock.Enabled)
		bedrock.SetModel(sj.Bedrock.Model)
		bedrock.SetAccessKeyID(sj.Bedrock.AccessKeyID)
		bedrock.SetSecretAccessKey(sj.Bedrock.SecretAccessKey)
		bedrock.SetRoleARN(sj.Bedrock.RoleARN)
		bedrock.SetProfile(sj.Bedrock.Profile)
		settings.SetBedrock(bedrock)
		// Reset updatedAt since SetBedrock updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.MCPServers) > 0 {
		mcpServers := entities.NewMCPServersSettings()
		for name, serverJSON := range sj.MCPServers {
			server := entities.NewMCPServer(name, serverJSON.Type)
			server.SetURL(serverJSON.URL)
			server.SetCommand(serverJSON.Command)
			server.SetArgs(serverJSON.Args)
			server.SetEnv(serverJSON.Env)
			server.SetHeaders(serverJSON.Headers)

			mcpServers.SetServer(name, server)
		}
		settings.SetMCPServers(mcpServers)
		// Reset updatedAt since SetMCPServers updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.Marketplaces) > 0 {
		marketplaces := entities.NewMarketplacesSettings()
		for name, mpJSON := range sj.Marketplaces {
			marketplace := entities.NewMarketplace(name)
			marketplace.SetURL(mpJSON.URL)
			marketplaces.SetMarketplace(name, marketplace)
		}
		settings.SetMarketplaces(marketplaces)
		// Reset updatedAt since SetMarketplaces updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.EnabledPlugins) > 0 {
		settings.SetEnabledPlugins(sj.EnabledPlugins)
		// Reset updatedAt since SetEnabledPlugins updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}
	if len(sj.Skills) > 0 {
		settings.SetSkills(sj.Skills)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.ClaudeCodeOAuthToken != "" {
		settings.SetClaudeCodeOAuthToken(sj.ClaudeCodeOAuthToken)
		// Reset updatedAt since SetClaudeCodeOAuthToken updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.AuthMode != "" {
		settings.SetAuthMode(entities.AuthMode(sj.AuthMode))
		// Reset updatedAt since SetAuthMode updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	codexConnection, connectionErr := r.decodeConnection(ctx, sj.CodexConnection)
	if connectionErr != nil {
		return nil, connectionErr
	}
	claudeConnection, connectionErr := r.decodeConnection(ctx, sj.ClaudeConnection)
	if connectionErr != nil {
		return nil, connectionErr
	}
	settings.SetCodexConnection(codexConnection)
	settings.SetClaudeConnection(claudeConnection)
	settings.SetUpdatedAt(sj.UpdatedAt)
	// Load env vars: encrypted_env_vars takes precedence; plain env_vars serves as
	// fallback for legacy data and is merged for keys not present in the encrypted map.
	{
		merged := make(map[string]string)
		// 1. Decrypt encrypted_env_vars
		if len(sj.EncryptedEnvVars) > 0 {
			for k, ev := range sj.EncryptedEnvVars {
				decSvc := r.decryptionSvc(domainservices.EncryptionMetadata{
					Algorithm:   ev.Algorithm,
					KeyID:       ev.KeyID,
					EncryptedAt: ev.EncryptedAt,
					Version:     ev.Version,
				})
				if decSvc == nil {
					log.Printf("[SETTINGS] No decryption service for env var %q (alg=%s kid=%s), skipping", k, ev.Algorithm, ev.KeyID)
					continue
				}
				plaintext, err := decSvc.Decrypt(ctx, &domainservices.EncryptedData{
					EncryptedValue: ev.EncryptedValue,
					Metadata: domainservices.EncryptionMetadata{
						Algorithm:   ev.Algorithm,
						KeyID:       ev.KeyID,
						EncryptedAt: ev.EncryptedAt,
						Version:     ev.Version,
					},
				})
				if err != nil {
					log.Printf("[SETTINGS] Failed to decrypt env var %q: %v, skipping", k, err)
					continue
				}
				merged[k] = plaintext
			}
		}
		// 2. Merge plain env_vars (backward compat; don't overwrite already-decrypted keys)
		for k, v := range sj.EnvVars {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
		if len(merged) > 0 {
			settings.SetEnvVars(merged)
			settings.SetUpdatedAt(sj.UpdatedAt)
		}
	}

	if sj.PreferredTeamID != "" {
		settings.SetPreferredTeamID(sj.PreferredTeamID)
		// Reset updatedAt since SetPreferredTeamID updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}
	if sj.GitHubAppInstallationID != "" {
		settings.SetGitHubAppInstallationID(sj.GitHubAppInstallationID)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.SlackUserID != "" {
		settings.SetSlackUserID(sj.SlackUserID)
		// Reset updatedAt since SetSlackUserID updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.NotificationChannels) > 0 {
		settings.SetNotificationChannels(sj.NotificationChannels)
		// Reset updatedAt since SetNotificationChannels updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}
	settings.SetGitHubRepositories(sj.GitHubRepositories)
	settings.SetUpdatedAt(sj.UpdatedAt)

	if len(sj.ExternalSessionManagers) > 0 {
		settings.SetExternalSessionManagers(sj.ExternalSessionManagers)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.DefaultSessionProfileID != "" {
		settings.SetDefaultSessionProfileID(sj.DefaultSessionProfileID)
	}
	settings.SetEnabledTeamSessionProfileIDs(sj.TeamProfileIDs)
	settings.SetUpdatedAt(sj.UpdatedAt)
	if sj.DefaultAgentType != "" {
		settings.SetDefaultAgentType(sj.DefaultAgentType)
	}
	if sj.AutoSuspend != nil {
		settings.SetAutoSuspend(sj.AutoSuspend)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	return settings, nil
}

// sanitizeSecretName sanitizes a string to be used as a Kubernetes Secret name
// Secret names must be lowercase, alphanumeric, and may contain dashes
// Example: "myorg/backend-team" -> "myorg-backend-team"
func sanitizeSecretName(s string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(s)
	// Replace non-alphanumeric characters (except dash) with dash
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")
	// Collapse multiple dashes
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Truncate to 253 characters (max Secret name length is 253)
	if len(sanitized) > 253-len(SettingsSecretPrefix) {
		sanitized = sanitized[:253-len(SettingsSecretPrefix)]
	}
	return sanitized
}

// sanitizeLabelValue sanitizes a string to be used as a Kubernetes label value
func sanitizeLabelValue(s string) string {
	// Label values must be 63 characters or less
	// Must start and end with alphanumeric character (or be empty)
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	// Remove leading/trailing invalid chars
	sanitized = strings.Trim(sanitized, "-_.")
	// Truncate to 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Ensure it ends with alphanumeric
	sanitized = strings.TrimRight(sanitized, "-_.")
	return sanitized
}
