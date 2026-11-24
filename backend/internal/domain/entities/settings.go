package entities

import (
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

const maxAutoSuspendMinutes = 7 * 24 * 60

// AutoSuspendSettings controls automatic suspension of idle persistent sessions.
type AutoSuspendSettings struct {
	Enabled            bool `json:"enabled"`
	IdleTimeoutMinutes int  `json:"idle_timeout_minutes"`
}

func (a *AutoSuspendSettings) Validate() error {
	if a == nil || !a.Enabled {
		return nil
	}
	if a.IdleTimeoutMinutes < 1 || a.IdleTimeoutMinutes > maxAutoSuspendMinutes {
		return fmt.Errorf("idle_timeout_minutes must be between 1 and %d", maxAutoSuspendMinutes)
	}
	return nil
}

// AuthMode represents the authentication mode for Claude Code
type AuthMode string

const (
	// AuthModeOAuth uses Claude Code OAuth token
	AuthModeOAuth AuthMode = "oauth"
	// AuthModeBedrock uses AWS Bedrock
	AuthModeBedrock             AuthMode = "bedrock"
	AuthModeAnthropicCompatible AuthMode = "anthropic_compatible"
)

// BedrockSettings represents AWS Bedrock configuration
type BedrockSettings struct {
	enabled         bool
	model           string
	accessKeyID     string
	secretAccessKey string
	roleARN         string
	profile         string
}

// NewBedrockSettings creates a new BedrockSettings
func NewBedrockSettings(enabled bool) *BedrockSettings {
	return &BedrockSettings{
		enabled: enabled,
	}
}

// Enabled returns whether Bedrock is enabled
func (b *BedrockSettings) Enabled() bool {
	return b.enabled
}

// Model returns the model ID
func (b *BedrockSettings) Model() string {
	return b.model
}

// AccessKeyID returns the AWS access key ID
func (b *BedrockSettings) AccessKeyID() string {
	return b.accessKeyID
}

// SecretAccessKey returns the AWS secret access key
func (b *BedrockSettings) SecretAccessKey() string {
	return b.secretAccessKey
}

// RoleARN returns the AWS role ARN for AssumeRole
func (b *BedrockSettings) RoleARN() string {
	return b.roleARN
}

// Profile returns the AWS profile name
func (b *BedrockSettings) Profile() string {
	return b.profile
}

// SetModel sets the model ID
func (b *BedrockSettings) SetModel(model string) {
	b.model = model
}

// SetAccessKeyID sets the AWS access key ID
func (b *BedrockSettings) SetAccessKeyID(accessKeyID string) {
	b.accessKeyID = accessKeyID
}

// SetSecretAccessKey sets the AWS secret access key
func (b *BedrockSettings) SetSecretAccessKey(secretAccessKey string) {
	b.secretAccessKey = secretAccessKey
}

// SetRoleARN sets the AWS role ARN
func (b *BedrockSettings) SetRoleARN(roleARN string) {
	b.roleARN = roleARN
}

// SetProfile sets the AWS profile name
func (b *BedrockSettings) SetProfile(profile string) {
	b.profile = profile
}

// Validate validates the BedrockSettings
func (b *BedrockSettings) Validate() error {
	return nil
}

// ExternalSessionManagerEntry represents a registered external session manager (External Session Manager)
type ExternalSessionManagerEntry struct {
	ID                  string            `json:"id"`
	InstanceID          string            `json:"instance_id,omitempty"`
	Name                string            `json:"name"`
	HMACSecret          string            `json:"hmac_secret,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	Version             string            `json:"version,omitempty"`
	ActiveSessions      int               `json:"active_sessions,omitempty"`
	LastHeartbeatAt     time.Time         `json:"last_heartbeat_at,omitempty"`
	EnrollmentTokenHash string            `json:"enrollment_token_hash,omitempty"`
	EnrollmentExpiresAt time.Time         `json:"enrollment_expires_at,omitempty"`
	Pool                string            `json:"pool,omitempty"`
	BindingSubjectType  string            `json:"binding_subject_type,omitempty"`
	BindingSubjectID    string            `json:"binding_subject_id,omitempty"`
	// PoolEnabled makes the manager's pool available for explicit selection.
	// A nil value preserves compatibility with settings saved before this field existed.
	PoolEnabled *bool `json:"pool_enabled,omitempty"`
	// AutomaticAssignmentEnabled also allows the resolver to select the pool implicitly.
	AutomaticAssignmentEnabled bool `json:"automatic_assignment_enabled,omitempty"`
	// LegacySchedulable preserves settings written by the prerelease schedulable API.
	LegacySchedulable bool `json:"schedulable,omitempty"`
	// LegacyDefault preserves backward compatibility with settings written before
	// automatic_assignment_enabled replaced default. It is never exposed by the public API.
	LegacyDefault bool `json:"default,omitempty"`
}

// IsAutomaticAssignmentEnabled accepts both former field names so existing
// installations keep routing sessions until their settings are saved again.
func (e ExternalSessionManagerEntry) IsAutomaticAssignmentEnabled() bool {
	return e.AutomaticAssignmentEnabled || e.LegacySchedulable || e.LegacyDefault
}

// IsPoolEnabled keeps previously auto-assignable managers available after the
// independent pool_enabled setting is introduced.
func (e ExternalSessionManagerEntry) IsPoolEnabled() bool {
	if e.PoolEnabled != nil {
		return *e.PoolEnabled
	}
	return e.IsAutomaticAssignmentEnabled()
}

// Settings represents user or team settings
type Settings struct {
	codexConnection         *modelprovider.Connection
	claudeConnection        *modelprovider.Connection
	name                    string
	bedrock                 *BedrockSettings
	mcpServers              *MCPServersSettings
	marketplaces            *MarketplacesSettings
	claudeCodeOAuthToken    string            // Claude Code OAuth token
	authMode                AuthMode          // Authentication mode (oauth or bedrock)
	enabledPlugins          []string          // plugin@marketplace format (e.g., "commit@claude-plugins-official")
	skills                  []string          // skills.sh package sources (e.g., "owner/repository")
	envVars                 map[string]string // Custom environment variables
	preferredTeamID         string            // "org/team-slug" format; if set, only this team's settings are used
	githubAppInstallationID string            // GitHub App installation ID used for team sessions
	githubRepositories      []string          // Additional repositories available to scoped sessions through the GitHub App broker
	slackUserID             string            // Slack DM notification user ID
	notificationChannels    []string          // Active notification channels (e.g. "web", "slack")
	externalSessionManagers []ExternalSessionManagerEntry
	defaultSessionProfileID string   // ID of the default session profile for this tenant
	enabledTeamProfileIDs   []string // Team profiles opted into personal-session selection
	defaultAgentType        string   // Agent type used when a session does not specify one
	autoSuspend             *AutoSuspendSettings
	secretSettings          []SecretSetting
	createdAt               time.Time
	updatedAt               time.Time
}

// SecretProjection controls whether one key from a SecretSetting is exposed to
// agent sessions. An empty Type keeps the value server-side only.
type SecretProjection struct {
	Key         string `json:"key"`
	Type        string `json:"type"` // "", "env", or "file"
	EnvName     string `json:"env_name,omitempty"`
	Path        string `json:"path,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

// SecretSetting is a named, settings-scoped collection of encrypted values.
// Values are never serialized directly by API response DTOs.
type SecretSetting struct {
	ID          string
	Name        string
	Values      map[string]string
	Projections []SecretProjection
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SecretSettings returns a deep copy of the configured secrets.
func (s *Settings) SecretSettings() []SecretSetting {
	result := make([]SecretSetting, len(s.secretSettings))
	for i := range s.secretSettings {
		result[i] = cloneSecretSetting(s.secretSettings[i])
	}
	return result
}

// SetSecretSettings replaces all settings-scoped secrets.
func (s *Settings) SetSecretSettings(values []SecretSetting) {
	s.secretSettings = make([]SecretSetting, len(values))
	for i := range values {
		s.secretSettings[i] = cloneSecretSetting(values[i])
	}
	s.updatedAt = time.Now()
}

func cloneSecretSetting(value SecretSetting) SecretSetting {
	result := value
	result.Values = make(map[string]string, len(value.Values))
	for key, item := range value.Values {
		result.Values[key] = item
	}
	result.Projections = append([]SecretProjection(nil), value.Projections...)
	return result
}

// NewSettings creates a new Settings
func NewSettings(name string) *Settings {
	now := time.Now()
	return &Settings{
		name:      name,
		createdAt: now,
		updatedAt: now,
	}
}

// Name returns the settings name (user or team name)
func (s *Settings) Name() string {
	return s.name
}

// SetName sets the settings name.
// This is used to populate the name from the URL parameter or storage metadata
// when it is missing from the stored JSON (e.g. for legacy entries).
func (s *Settings) SetName(name string) {
	s.name = name
}

// Bedrock returns the Bedrock settings
func (s *Settings) Bedrock() *BedrockSettings {
	return s.bedrock
}

// CreatedAt returns the creation time
func (s *Settings) CreatedAt() time.Time {
	return s.createdAt
}

// UpdatedAt returns the last update time
func (s *Settings) UpdatedAt() time.Time {
	return s.updatedAt
}

// SetBedrock sets the Bedrock settings
func (s *Settings) SetBedrock(bedrock *BedrockSettings) {
	s.bedrock = bedrock
	s.updatedAt = time.Now()
}

// MCPServers returns the MCP servers settings
func (s *Settings) MCPServers() *MCPServersSettings {
	return s.mcpServers
}

// SetMCPServers sets the MCP servers settings
func (s *Settings) SetMCPServers(mcpServers *MCPServersSettings) {
	s.mcpServers = mcpServers
	s.updatedAt = time.Now()
}

// Marketplaces returns the marketplaces settings
func (s *Settings) Marketplaces() *MarketplacesSettings {
	return s.marketplaces
}

// SetMarketplaces sets the marketplaces settings
func (s *Settings) SetMarketplaces(marketplaces *MarketplacesSettings) {
	s.marketplaces = marketplaces
	s.updatedAt = time.Now()
}

// EnabledPlugins returns the list of enabled plugins in "plugin@marketplace" format
func (s *Settings) EnabledPlugins() []string {
	return s.enabledPlugins
}

// SetEnabledPlugins sets the list of enabled plugins in "plugin@marketplace" format
func (s *Settings) SetEnabledPlugins(plugins []string) {
	s.enabledPlugins = append([]string(nil), plugins...)
	s.updatedAt = time.Now()
}

// Skills returns the configured skills.sh package sources.
func (s *Settings) Skills() []string { return append([]string(nil), s.skills...) }

// SetSkills replaces the configured skills.sh package sources.
func (s *Settings) SetSkills(skills []string) {
	s.skills = append([]string(nil), skills...)
	s.updatedAt = time.Now()
}

// SetCreatedAt sets the creation time (for loading from storage)
func (s *Settings) SetCreatedAt(t time.Time) {
	s.createdAt = t
}

// SetUpdatedAt sets the update time (for loading from storage)
func (s *Settings) SetUpdatedAt(t time.Time) {
	s.updatedAt = t
}

// ClaudeCodeOAuthToken returns the Claude Code OAuth token
func (s *Settings) ClaudeCodeOAuthToken() string {
	return s.claudeCodeOAuthToken
}

// SetClaudeCodeOAuthToken sets the Claude Code OAuth token
func (s *Settings) SetClaudeCodeOAuthToken(token string) {
	s.claudeCodeOAuthToken = token
	s.updatedAt = time.Now()
}

// HasClaudeCodeOAuthToken returns true if a Claude Code OAuth token is set
func (s *Settings) HasClaudeCodeOAuthToken() bool {
	return s.claudeCodeOAuthToken != ""
}

// AuthMode returns the authentication mode
func (s *Settings) AuthMode() AuthMode {
	return s.authMode
}

// SetAuthMode sets the authentication mode
