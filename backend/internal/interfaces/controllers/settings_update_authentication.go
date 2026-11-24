package controllers

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (c *SettingsController) applyBedrockSettings(settings *entities.Settings, req *UpdateSettingsRequest) {
	// Update Bedrock settings
	if req.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(req.Bedrock.Enabled)
		bedrock.SetModel(req.Bedrock.Model)

		// Preserve existing credentials if new values are empty
		existingBedrock := settings.Bedrock()

		accessKeyID := req.Bedrock.AccessKeyID
		if accessKeyID == "" && existingBedrock != nil {
			accessKeyID = existingBedrock.AccessKeyID()
		}
		bedrock.SetAccessKeyID(accessKeyID)

		secretAccessKey := req.Bedrock.SecretAccessKey
		if secretAccessKey == "" && existingBedrock != nil {
			secretAccessKey = existingBedrock.SecretAccessKey()
		}
		bedrock.SetSecretAccessKey(secretAccessKey)

		roleARN := req.Bedrock.RoleARN
		if roleARN == "" && existingBedrock != nil {
			roleARN = existingBedrock.RoleARN()
		}
		bedrock.SetRoleARN(roleARN)

		profile := req.Bedrock.Profile
		if profile == "" && existingBedrock != nil {
			profile = existingBedrock.Profile()
		}
		bedrock.SetProfile(profile)

		settings.SetBedrock(bedrock)
	}
}

func (c *SettingsController) applyBasicSettings(settings *entities.Settings, req *UpdateSettingsRequest) {
	// Update enabled plugins
	if req.EnabledPlugins != nil {
		settings.SetEnabledPlugins(req.EnabledPlugins)
	}
	if req.Skills != nil {
		settings.SetSkills(req.Skills)
	}

	// Update environment variables
	if req.EnvVars != nil {
		// Get existing env vars for preserving empty values
		existingEnvVars := settings.EnvVars()

		// Use mergeSecrets helper to preserve existing values when new value is empty
		envVars := c.mergeSecrets(existingEnvVars, req.EnvVars)
		settings.SetEnvVars(envVars)
	}

	// Update Claude Code OAuth Token
	if req.ClaudeCodeOAuthToken != nil {
		settings.SetClaudeCodeOAuthToken(*req.ClaudeCodeOAuthToken)
	}
}
