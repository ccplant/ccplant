package entities

import (
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func (s *Settings) SetAuthMode(mode AuthMode) {
	s.authMode = mode
	s.updatedAt = time.Now()
}

// EnvVars returns the custom environment variables
func (s *Settings) EnvVars() map[string]string {
	return s.envVars
}

// SetEnvVars sets the custom environment variables
func (s *Settings) SetEnvVars(envVars map[string]string) {
	if envVars == nil {
		s.envVars = make(map[string]string)
	} else {
		s.envVars = envVars
	}
	s.updatedAt = time.Now()
}

// EnvVarKeys returns a sorted list of environment variable keys (values are not included)
func (s *Settings) EnvVarKeys() []string {
	if len(s.envVars) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.envVars))
	for k := range s.envVars {
		keys = append(keys, k)
	}
	// Sort keys for consistent output
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// PreferredTeamID returns the preferred team ID
func (s *Settings) PreferredTeamID() string {
	return s.preferredTeamID
}

// SetPreferredTeamID sets the preferred team ID
func (s *Settings) SetPreferredTeamID(id string) {
	s.preferredTeamID = id
	s.updatedAt = time.Now()
}

// GitHubAppInstallationID returns the GitHub App installation ID for this settings scope.
func (s *Settings) GitHubAppInstallationID() string {
	return s.githubAppInstallationID
}

// SetGitHubAppInstallationID sets the GitHub App installation ID for this settings scope.
func (s *Settings) SetGitHubAppInstallationID(id string) {
	s.githubAppInstallationID = id
	s.updatedAt = time.Now()
}

// SlackUserID returns the Slack user ID for DM notifications
func (s *Settings) SlackUserID() string {
	return s.slackUserID
}

// SetSlackUserID sets the Slack user ID for DM notifications
func (s *Settings) SetSlackUserID(id string) {
	s.slackUserID = id
	s.updatedAt = time.Now()
}

// NotificationChannels returns the list of active notification channels
func (s *Settings) NotificationChannels() []string {
	return s.notificationChannels
}

// SetNotificationChannels sets the list of active notification channels
func (s *Settings) SetNotificationChannels(channels []string) {
	s.notificationChannels = channels
	s.updatedAt = time.Now()
}

// ExternalSessionManagers returns the list of registered external session managers
func (s *Settings) ExternalSessionManagers() []ExternalSessionManagerEntry {
	return s.externalSessionManagers
}

// SetExternalSessionManagers sets the list of external session managers
func (s *Settings) SetExternalSessionManagers(managers []ExternalSessionManagerEntry) {
	s.externalSessionManagers = managers
	s.updatedAt = time.Now()
}

// Validate validates the Settings
func (s *Settings) Validate() error {
	// Note: name is not validated here because it is always set from the URL parameter
	// or derived from Kubernetes Secret metadata, not from the stored JSON.

	if s.bedrock != nil {
		if err := s.bedrock.Validate(); err != nil {
			return err
		}
	}

	if s.mcpServers != nil {
		if err := s.mcpServers.Validate(); err != nil {
			return err
		}
	}

	if s.marketplaces != nil {
		if err := s.marketplaces.Validate(); err != nil {
			return err
		}
	}
	if err := s.autoSuspend.Validate(); err != nil {
		return fmt.Errorf("invalid auto_suspend settings: %w", err)
	}

	return nil
}

// DefaultSessionProfileID returns the default session profile ID for this tenant
func (s *Settings) DefaultSessionProfileID() string {
	return s.defaultSessionProfileID
}

// SetDefaultSessionProfileID sets the default session profile ID for this tenant
func (s *Settings) SetDefaultSessionProfileID(id string) {
	s.defaultSessionProfileID = id
	s.updatedAt = time.Now()
}

// EnabledTeamSessionProfileIDs returns team profiles opted into personal use.
func (s *Settings) EnabledTeamSessionProfileIDs() []string {
	return append([]string(nil), s.enabledTeamProfileIDs...)
}

// SetEnabledTeamSessionProfileIDs replaces the team profiles opted into personal use.
func (s *Settings) SetEnabledTeamSessionProfileIDs(ids []string) {
	s.enabledTeamProfileIDs = append([]string(nil), ids...)
	s.updatedAt = time.Now()
}

// GitHubRepositories returns repositories that sessions in this settings scope may access through the GitHub App broker.
func (s *Settings) GitHubRepositories() []string {
	return append([]string(nil), s.githubRepositories...)
}

// SetGitHubRepositories replaces the repositories available to team sessions.
func (s *Settings) SetGitHubRepositories(repositories []string) {
	s.githubRepositories = append([]string(nil), repositories...)
	s.updatedAt = time.Now()
}

// DefaultAgentType returns the agent type used for sessions that do not specify one.
func (s *Settings) DefaultAgentType() string { return s.defaultAgentType }

// SetDefaultAgentType sets the agent type used for sessions that do not specify one.
func (s *Settings) SetDefaultAgentType(agentType string) {
	s.defaultAgentType = agentType
	s.updatedAt = time.Now()
}

// AutoSuspend returns a copy of the automatic suspension policy.
func (s *Settings) AutoSuspend() *AutoSuspendSettings {
	if s.autoSuspend == nil {
		return nil
	}
	copy := *s.autoSuspend
	return &copy
}

// SetAutoSuspend sets the automatic suspension policy.
func (s *Settings) SetAutoSuspend(value *AutoSuspendSettings) {
	if value == nil {
		s.autoSuspend = nil
	} else {
		copy := *value
		s.autoSuspend = &copy
	}
	s.updatedAt = time.Now()
}

func (s *Settings) CodexConnection() *modelprovider.Connection  { return s.codexConnection.Clone() }
func (s *Settings) ClaudeConnection() *modelprovider.Connection { return s.claudeConnection.Clone() }
func (s *Settings) SetCodexConnection(c *modelprovider.Connection) {
	s.codexConnection = c.Clone()
	s.updatedAt = time.Now()
}
func (s *Settings) SetClaudeConnection(c *modelprovider.Connection) {
	s.claudeConnection = c.Clone()
	if c != nil {
		s.authMode = AuthMode(c.Mode)
	}
	s.updatedAt = time.Now()
}
