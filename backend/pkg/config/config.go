// Package config provides configuration management for agentapi-proxy using viper.
//
// Configuration can be loaded from:
//   - JSON files (backward compatibility)
//   - YAML files
//   - Environment variables with AGENTAPI_ prefix
//
// Environment variable examples:
//
//	AGENTAPI_START_PORT=8080
//	AGENTAPI_AUTH_ENABLED=true
//	AGENTAPI_AUTH_STATIC_ENABLED=true
//	AGENTAPI_AUTH_STATIC_HEADER_NAME=X-API-Key
//	AGENTAPI_AUTH_STATIC_KEYS_FILE=/path/to/keys.json
//	AGENTAPI_AUTH_GITHUB_ENABLED=true
//	AGENTAPI_AUTH_GITHUB_BASE_URL=https://api.github.com
//	AGENTAPI_AUTH_GITHUB_TOKEN_HEADER=Authorization
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID=your_client_id
//	AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET=your_client_secret
//	AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE=read:user read:org project
//	AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE=user
//	AGENTAPI_ENABLE_MULTIPLE_USERS=true
//	AGENTAPI_WEBHOOK_BASE_URL=https://example.com
//	AGENTAPI_WEBHOOK_GITHUB_ENTERPRISE_HOST=github.enterprise.com
//
// Configuration file search paths:
//   - Current directory
//   - $HOME/.agentapi/
//   - /etc/agentapi/
//
// Configuration file names: config.json, config.yaml, config.yml
package config

// AuthConfig represents authentication configuration
type AuthConfig struct {
	// AdminKey enables a single non-expiring admin API key via the
	// X-API-Key header. Prefer AGENTAPI_AUTH_ADMIN_KEY over config files so
	// the value never lands in version control.
	AdminKey       string                    `json:"admin_key,omitempty" mapstructure:"admin_key"`
	BootstrapAdmin *BootstrapAdminAuthConfig `json:"bootstrap_admin,omitempty" mapstructure:"bootstrap_admin"`
	GitHub         *GitHubAuthConfig         `json:"github,omitempty" mapstructure:"github"`
}

// BootstrapAdminAuthConfig provides a break-glass administrator identity that
// is available before any external authentication provider is configured.
type BootstrapAdminAuthConfig struct {
	Enabled  bool   `json:"enabled" mapstructure:"enabled"`
	UserID   string `json:"user_id" mapstructure:"user_id"`
	Username string `json:"username" mapstructure:"username"`
	Token    string `json:"token" mapstructure:"token"`
}

// GitHubAuthConfig represents GitHub OAuth authentication
type GitHubAuthConfig struct {
	Enabled      bool               `json:"enabled" mapstructure:"enabled"`
	ConnectionID string             `json:"connection_id,omitempty" mapstructure:"connection_id"`
	BaseURL      string             `json:"base_url" mapstructure:"base_url"`
	TokenHeader  string             `json:"token_header" mapstructure:"token_header"`
	UserMapping  GitHubUserMapping  `json:"user_mapping" mapstructure:"user_mapping"`
	OAuth        *GitHubOAuthConfig `json:"oauth,omitempty" mapstructure:"oauth"`
	// TeamDiscoveryPatterns participate in membership fetching but not role mapping.
	TeamDiscoveryPatterns []string `json:"-" mapstructure:"-"`
}

// TeamDiscoveryRule creates/adopts a ccplant team when a GitHub membership
// matches TeamPattern. The initial implementation preserves the matched
// organization/team-slug as the team key.
type TeamDiscoveryRule struct {
	TeamPattern string `json:"team_pattern" mapstructure:"team_pattern" yaml:"team_pattern"`
}

// GitHubOAuthConfig represents GitHub OAuth2 configuration
type GitHubOAuthConfig struct {
	ClientID     string `json:"client_id" mapstructure:"client_id"`
	ClientSecret string `json:"client_secret" mapstructure:"client_secret"`
	Scope        string `json:"scope" mapstructure:"scope"`
	BaseURL      string `json:"base_url,omitempty" mapstructure:"base_url"`
}

// GitHubUserMapping represents user role mapping configuration
type GitHubUserMapping struct {
	DefaultRole           string                  `json:"default_role" mapstructure:"default_role" yaml:"default_role"`
	DefaultPermissions    []string                `json:"default_permissions" mapstructure:"default_permissions" yaml:"default_permissions"`
	AllowUsersWithoutTeam bool                    `json:"allow_users_without_team" mapstructure:"allow_users_without_team" yaml:"allow_users_without_team"`
	TeamRoleMapping       map[string]TeamRoleRule `json:"team_role_mapping" mapstructure:"team_role_mapping" yaml:"team_role_mapping"`
}

// TeamRoleRule represents a team-based role rule
type TeamRoleRule struct {
	Role        string   `json:"role" mapstructure:"role" yaml:"role"`
	Permissions []string `json:"permissions" mapstructure:"permissions" yaml:"permissions"`
	EnvFile     string   `json:"env_file,omitempty" mapstructure:"env_file" yaml:"env_file"`
}

// RoleEnvFilesConfig represents role-based environment files configuration
type RoleEnvFilesConfig struct {
	// Enabled enables role-based environment file loading
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// Path is the directory path containing role-specific .env files
	Path string `json:"path" mapstructure:"path"`
	// LoadDefault loads default.env before role-specific env file
	LoadDefault bool `json:"load_default" mapstructure:"load_default"`
}
