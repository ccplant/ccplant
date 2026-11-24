package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/urlutil"
)

func (c *SettingsController) DeleteSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := urlutil.DecodeSlashParam(ctx.Param("name"))
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	settings, _ := c.repo.FindByName(ctx.Request().Context(), name)
	err := c.repo.Delete(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete settings")
	}
	if settings != nil {
		for _, manager := range settings.ExternalSessionManagers() {
			if manager.HMACSecret != "" {
				if cleanupErr := c.deleteExternalManagerRegistration(ctx.Request().Context(), manager); cleanupErr != nil {
					return echo.NewHTTPError(http.StatusConflict, "settings deleted but manager cleanup failed").SetInternal(cleanupErr)
				}
			}
		}
	}

	return ctx.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// canAccess checks if the user can access settings for the given name
func (c *SettingsController) canAccess(user *entities.User, name string) bool {
	log.Printf("[SETTINGS_ACCESS] Checking access for user=%s, userType=%s, requestedName=%s", user.ID(), user.UserType(), name)

	// "base" is admin-only (global base settings)
	if name == BaseSettingsName {
		if user.IsAdmin() {
			log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s is admin, accessing base settings", user.ID())
			return true
		}
		log.Printf("[SETTINGS_ACCESS] DENIED: user=%s is not admin, base settings are admin-only", user.ID())
		return false
	}

	// Check if it's the user's own settings
	if string(user.ID()) == name {
		log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s owns settings (userID match)", user.ID())
		return true
	}
	// Administrators may manage shared team settings, but personal settings
	// remain private even from the administrator role.
	if user.IsAdmin() && strings.Contains(name, "/") {
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		teams := user.GitHubInfo().Teams()
		log.Printf("[SETTINGS_ACCESS] User has %d teams", len(teams))
		for _, team := range teams {
			teamName := team.Organization + "/" + team.TeamSlug
			log.Printf("[SETTINGS_ACCESS] Checking team: name=%q, role=%s", teamName, team.Role)
			if teamName == name {
				log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	} else {
		log.Printf("[SETTINGS_ACCESS] User has no GitHubInfo")
	}

	log.Printf("[SETTINGS_ACCESS] DENIED: user=%s has no access to settings %q", user.ID(), name)
	return false
}

// canModify checks if the user can modify settings for the given name
func (c *SettingsController) canModify(user *entities.User, name string) bool {
	log.Printf("[SETTINGS_MODIFY] Checking modify permission for user=%s, userType=%s, requestedName=%s", user.ID(), user.UserType(), name)

	// "base" is admin-only (global base settings)
	if name == BaseSettingsName {
		if user.IsAdmin() {
			log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s is admin, modifying base settings", user.ID())
			return true
		}
		log.Printf("[SETTINGS_MODIFY] DENIED: user=%s is not admin, base settings are admin-only", user.ID())
		return false
	}

	// Check if it's the user's own settings
	if string(user.ID()) == name {
		log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s owns settings (userID match)", user.ID())
		return true
	}
	if user.IsAdmin() && strings.Contains(name, "/") {
		return true
	}

	// Check if user belongs to the team (any team member can modify)
	if user.GitHubInfo() != nil {
		teams := user.GitHubInfo().Teams()
		log.Printf("[SETTINGS_MODIFY] User has %d teams", len(teams))
		for _, team := range teams {
			teamName := team.Organization + "/" + team.TeamSlug
			log.Printf("[SETTINGS_MODIFY] Checking team: name=%q, role=%s", teamName, team.Role)
			if teamName == name {
				log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	} else {
		log.Printf("[SETTINGS_MODIFY] User has no GitHubInfo")
	}

	log.Printf("[SETTINGS_MODIFY] DENIED: user=%s has no modify permission for settings %q", user.ID(), name)
	return false
}

// determineAuthMode determines the auth mode based on request and available credentials
func (c *SettingsController) determineAuthMode(settings *entities.Settings, requestedMode *string) entities.AuthMode {
	if connection := settings.ClaudeConnection(); connection != nil {
		return entities.AuthMode(connection.Mode)
	}
	// 1. If explicitly specified, use that mode (if credentials are available)
	if requestedMode != nil && *requestedMode != "" {
		mode := entities.AuthMode(*requestedMode)
		switch mode {
		case entities.AuthModeOAuth:
			if settings.HasClaudeCodeOAuthToken() {
				return entities.AuthModeOAuth
			}
		case entities.AuthModeBedrock:
			if bedrock := settings.Bedrock(); bedrock != nil && bedrock.Enabled() {
				return entities.AuthModeBedrock
			}
		}
		// Requested mode's credentials not available, fall through to auto-detection
	}

	// 2. Auto-detect: OAuth takes priority
	if settings.HasClaudeCodeOAuthToken() {
		return entities.AuthModeOAuth
	}
	if bedrock := settings.Bedrock(); bedrock != nil && bedrock.Enabled() {
		return entities.AuthModeBedrock
	}

	// 3. No credentials available
	return ""
}

// toResponse converts Settings entity to response
func (c *SettingsController) toResponse(settings *entities.Settings) *SettingsResponse {
	resp := &SettingsResponse{
		CodexConnection:         settings.CodexConnection(),
		ClaudeConnection:        settings.ClaudeConnection(),
		DefaultOpenAIBaseURL:    strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")),
		DefaultAnthropicBaseURL: strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")),
		Name:                    settings.Name(),
		HasClaudeCodeOAuthToken: settings.HasClaudeCodeOAuthToken(),
		AuthMode:                string(settings.AuthMode()),
		CreatedAt:               settings.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:               settings.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		resp.Bedrock = &BedrockSettingsResponse{
			Enabled: bedrock.Enabled(),
			Model:   bedrock.Model(),
			// AccessKeyID and SecretAccessKey are not returned for security reasons
			RoleARN: bedrock.RoleARN(),
			Profile: bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		resp.MCPServers = make(map[string]*MCPServerResponse)
		for name, server := range mcpServers.Servers() {
			resp.MCPServers[name] = &MCPServerResponse{
				Type:       server.Type(),
				URL:        server.URL(),
				Command:    server.Command(),
				Args:       server.Args(),
				EnvKeys:    server.EnvKeys(),
				HeaderKeys: server.HeaderKeys(),
			}
		}
	}

	if marketplaces := settings.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		resp.Marketplaces = make(map[string]*MarketplaceResponse)
		for name, marketplace := range marketplaces.Marketplaces() {
			resp.Marketplaces[name] = &MarketplaceResponse{
				URL: marketplace.URL(),
			}
		}
	}

	if plugins := settings.EnabledPlugins(); len(plugins) > 0 {
		resp.EnabledPlugins = plugins
	}
	if skills := settings.Skills(); len(skills) > 0 {
		resp.Skills = skills
	}

	if envVarKeys := settings.EnvVarKeys(); len(envVarKeys) > 0 {
		resp.EnvVarKeys = envVarKeys
	}

	resp.PreferredTeamID = settings.PreferredTeamID()
	resp.GitHubAppInstallationID = settings.GitHubAppInstallationID()
	resp.GitHubRepositories = settings.GitHubRepositories()
	resp.SlackUserID = settings.SlackUserID()
	resp.NotificationChannels = settings.NotificationChannels()
	resp.DefaultSessionProfileID = settings.DefaultSessionProfileID()
	resp.EnabledTeamProfileIDs = settings.EnabledTeamSessionProfileIDs()
	resp.DefaultAgentType = settings.DefaultAgentType()
	resp.AutoSuspend = settings.AutoSuspend()

	// External session managers: never return the HMAC secret — indicate only whether one is set.
	if managers := settings.ExternalSessionManagers(); len(managers) > 0 {
		resp.ExternalSessionManagers = make([]ExternalSessionManagerResponse, 0, len(managers))
		for _, m := range managers {
			if m.HMACSecret == "" {
				continue
			}
			resp.ExternalSessionManagers = append(resp.ExternalSessionManagers, ExternalSessionManagerResponse{
				ID:                 m.ID,
				InstanceID:         m.InstanceID,
				Name:               m.Name,
				HasConnectionToken: m.HMACSecret != "",
				Labels:             m.Labels,
				Version:            m.Version,
				ActiveSessions:     m.ActiveSessions,
				LastHeartbeatAt:    timePtrUnlessZero(m.LastHeartbeatAt),
			})
		}
	}

	return resp
}

func isValidDefaultAgentType(agentType string) bool {
	switch agentType {
	case "", "auto", "claude-acp", "codex-acp", "pi", "cursor", "antigravity":
		return true
	default:
		return false
	}
}

// mergeSecrets merges existing and new secret maps.
// Keys in new with non-empty values are added or updated.
// Keys in new with empty string values are deleted from the result.
// Keys in existing that are not present in new are preserved.
func (c *SettingsController) mergeSecrets(existing, new map[string]string) map[string]string {
	if new == nil {
		return existing
	}
	if existing == nil {
		// Filter out empty strings (nothing to delete when existing is nil)
		result := make(map[string]string)
		for k, v := range new {
			if v != "" {
				result[k] = v
			}
		}
		return result
	}

	// Start with all existing keys preserved
	result := make(map[string]string)
	for k, v := range existing {
		result[k] = v
	}

	// Apply changes from new (empty strings delete the key)
	for k, v := range new {
		if v != "" {
			// Non-empty value adds or updates the key
			result[k] = v
		} else {
			// Empty string deletes the key
			delete(result, k)
		}
	}
	return result
}

// generateSettingsESMSecret generates a random hex HMAC secret of the given byte length
func generateSettingsESMSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
