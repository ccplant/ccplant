package controllers

import (
	"encoding/json"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

// BedrockSettingsRequest is the request body for Bedrock settings
type BedrockSettingsRequest struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// MCPServerRequest is the request body for a single MCP server
type MCPServerRequest struct {
	Type    string            `json:"type"`              // "stdio", "http", "sse"
	URL     string            `json:"url,omitempty"`     // for http/sse
	Command string            `json:"command,omitempty"` // for stdio
	Args    []string          `json:"args,omitempty"`    // for stdio
	Env     map[string]string `json:"env,omitempty"`     // environment variables
	Headers map[string]string `json:"headers,omitempty"` // for http/sse
}

// MarketplaceRequest is the request body for a single marketplace
type MarketplaceRequest struct {
	URL string `json:"url"`
}

// UpdateSettingsRequest is the request body for updating settings
type UpdateSettingsRequest struct {
	CodexConnection         map[string]json.RawMessage       `json:"codex_connection,omitempty"`
	ClaudeConnection        map[string]json.RawMessage       `json:"claude_connection,omitempty"`
	Bedrock                 *BedrockSettingsRequest          `json:"bedrock"`
	MCPServers              map[string]*MCPServerRequest     `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*MarketplaceRequest   `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken    *string                          `json:"claude_code_oauth_token,omitempty"`
	AuthMode                *string                          `json:"auth_mode,omitempty"`                  // "oauth" or "bedrock"
	EnabledPlugins          []string                         `json:"enabled_plugins,omitempty"`            // plugin@marketplace format
	Skills                  []string                         `json:"skills,omitempty"`                     // skills.sh package sources
	EnvVars                 map[string]string                `json:"env_vars,omitempty"`                   // Custom environment variables
	PreferredTeamID         *string                          `json:"preferred_team_id,omitempty"`          // "org/team-slug" format; "" to clear
	GitHubAppInstallationID *string                          `json:"github_app_installation_id,omitempty"` // Team GitHub App installation ID; "" to clear
	GitHubRepositories      *[]string                        `json:"github_repositories,omitempty"`
	SlackUserID             *string                          `json:"slack_user_id,omitempty"`              // Slack DM notification user ID
	NotificationChannels    *[]string                        `json:"notification_channels,omitempty"`      // Active notification channels (e.g. ["web", "slack"])
	ExternalSessionManagers *[]ExternalSessionManagerRequest `json:"external_session_managers,omitempty"`  // External session managers (External Session Manager registrations)
	DefaultSessionProfileID *string                          `json:"default_session_profile_id,omitempty"` // Default session profile ID for this settings scope
	EnabledTeamProfileIDs   *[]string                        `json:"enabled_team_session_profile_ids,omitempty"`
	DefaultAgentType        *string                          `json:"default_agent_type,omitempty"` // Default agent type for sessions in this settings scope
	AutoSuspend             *entities.AutoSuspendSettings    `json:"auto_suspend,omitempty"`
}

// ExternalSessionManagerRequest represents updates to an already-enrolled manager.
type ExternalSessionManagerRequest struct {
	ID         string            `json:"id"`                    // Required; registration uses the enrollment-token API
	InstanceID string            `json:"instance_id,omitempty"` // Stable native host installation ID
	Name       string            `json:"name"`                  // Human-readable name
	Labels     map[string]string `json:"labels,omitempty"`      // Matches allocator.* session tags
}

// BedrockSettingsResponse is the response body for Bedrock settings
type BedrockSettingsResponse struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// MCPServerResponse is the response body for a single MCP server
type MCPServerResponse struct {
	Type       string   `json:"type"`
	URL        string   `json:"url,omitempty"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	EnvKeys    []string `json:"env_keys,omitempty"`    // only keys, not values
	HeaderKeys []string `json:"header_keys,omitempty"` // only keys, not values
}

// MarketplaceResponse is the response body for a single marketplace
type MarketplaceResponse struct {
	URL string `json:"url"`
}

// SettingsResponse is the response body for settings
type SettingsResponse struct {
	CodexConnection         *modelprovider.Connection        `json:"codex_connection,omitempty"`
	ClaudeConnection        *modelprovider.Connection        `json:"claude_connection,omitempty"`
	DefaultOpenAIBaseURL    string                           `json:"default_openai_base_url,omitempty"`
	DefaultAnthropicBaseURL string                           `json:"default_anthropic_base_url,omitempty"`
	Name                    string                           `json:"name"`
	Bedrock                 *BedrockSettingsResponse         `json:"bedrock,omitempty"`
	MCPServers              map[string]*MCPServerResponse    `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*MarketplaceResponse  `json:"marketplaces,omitempty"`
	HasClaudeCodeOAuthToken bool                             `json:"has_claude_code_oauth_token"`
	AuthMode                string                           `json:"auth_mode,omitempty"`
	EnabledPlugins          []string                         `json:"enabled_plugins,omitempty"`   // plugin@marketplace format
	Skills                  []string                         `json:"skills,omitempty"`            // skills.sh package sources
	EnvVarKeys              []string                         `json:"env_var_keys,omitempty"`      // only keys, not values
	PreferredTeamID         string                           `json:"preferred_team_id,omitempty"` // "org/team-slug" format
	GitHubAppInstallationID string                           `json:"github_app_installation_id,omitempty"`
	GitHubRepositories      []string                         `json:"github_repositories,omitempty"`
	SlackUserID             string                           `json:"slack_user_id,omitempty"`              // Slack DM notification user ID
	NotificationChannels    []string                         `json:"notification_channels,omitempty"`      // Active notification channels
	ExternalSessionManagers []ExternalSessionManagerResponse `json:"external_session_managers,omitempty"`  // Registered external session managers
	DefaultSessionProfileID string                           `json:"default_session_profile_id,omitempty"` // Default session profile ID for this settings scope
	EnabledTeamProfileIDs   []string                         `json:"enabled_team_session_profile_ids,omitempty"`
	DefaultAgentType        string                           `json:"default_agent_type,omitempty"` // Default agent type for sessions in this settings scope
	AutoSuspend             *entities.AutoSuspendSettings    `json:"auto_suspend,omitempty"`
	CreatedAt               string                           `json:"created_at"`
	UpdatedAt               string                           `json:"updated_at"`
}

// ExternalSessionManagerResponse represents a single external session manager in responses
type ExternalSessionManagerResponse struct {
	ID                 string            `json:"id"`
	InstanceID         string            `json:"instance_id,omitempty"`
	Name               string            `json:"name"`
	HasConnectionToken bool              `json:"has_connection_token"`       // true if a connection token is configured
	ConnectionToken    string            `json:"connection_token,omitempty"` // returned only immediately after generation or rotation
	Labels             map[string]string `json:"labels,omitempty"`
	Version            string            `json:"version,omitempty"`
	ActiveSessions     int               `json:"active_sessions,omitempty"`
	LastHeartbeatAt    *time.Time        `json:"last_heartbeat_at,omitempty"`
}

// AvailableManagerEntry represents a single available ESM entry returned by GET /settings/managers
type AvailableManagerEntry struct {
	ID                         string `json:"id"`
	Name                       string `json:"name"`
	Pool                       string `json:"pool"`
	PoolEnabled                bool   `json:"pool_enabled,omitempty"`
	AutomaticAssignmentEnabled bool   `json:"automatic_assignment_enabled,omitempty"`
	Source                     string `json:"source"`      // "user" or "team"
	SourceName                 string `json:"source_name"` // user ID or team ID
}

// AvailableManagersResponse is the response body for GET /settings/managers
type AvailableManagersResponse struct {
	Managers []AvailableManagerEntry `json:"managers"`
}
