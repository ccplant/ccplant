package config

// WebhookConfig represents webhook configuration
type WebhookConfig struct {
	// BaseURL is the base URL for webhook endpoints (e.g., "https://example.com")
	// If not set, the URL will be auto-detected from incoming request headers
	BaseURL string `json:"base_url" mapstructure:"base_url"`
	// GitHubEnterpriseHost is the default GitHub Enterprise host for webhook matching
	// When set, webhooks without explicit enterprise_url will match against this host
	// Example: "github.enterprise.com" (hostname only, without https://)
	GitHubEnterpriseHost string `json:"github_enterprise_host" mapstructure:"github_enterprise_host"`
}

// SciaConfig represents scia OAuth broker/proxy integration configuration.
type SciaConfig struct {
	// Enabled controls whether scia integration is exposed to clients and injected into sessions.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// PublicBaseURL is the browser-reachable origin that serves /oauth and /_scia.
	// When empty, UI clients can still use same-origin relative OAuth URLs.
	PublicBaseURL string `json:"public_base_url" mapstructure:"public_base_url"`
	// OAuthInternalURL is the proxy-reachable scia OAuth server origin used for metadata.
	OAuthInternalURL string `json:"oauth_internal_url" mapstructure:"oauth_internal_url"`
	// ProxyURL is the forward proxy URL used by session Pods for outbound Google API calls.
	ProxyURL string `json:"proxy_url" mapstructure:"proxy_url"`
	// Credential is the scia credential ID used for Google OAuth, e.g. "takutakahashi.google".
	Credential string `json:"credential" mapstructure:"credential"`
	// UserNamespace is the scia user namespace used by the authorization-url endpoint.
	UserNamespace string `json:"user_namespace" mapstructure:"user_namespace"`
	// NoProxy is appended to the session NO_PROXY value when ProxyURL is injected.
	NoProxy string `json:"no_proxy" mapstructure:"no_proxy"`
	// SessionSidecarEnabled runs scia as a sidecar in each session Pod.
	SessionSidecarEnabled bool `json:"session_sidecar_enabled" mapstructure:"session_sidecar_enabled"`
	// SessionSidecarImage is the scia image used by the session sidecar.
	SessionSidecarImage string `json:"session_sidecar_image" mapstructure:"session_sidecar_image"`
	// SessionSidecarConfigImage is the shell-capable image used to render scia sidecar config.
	SessionSidecarConfigImage string `json:"session_sidecar_config_image" mapstructure:"session_sidecar_config_image"`
	// SessionSidecarPort is the localhost HTTP proxy port exposed by the sidecar.
	SessionSidecarPort int `json:"session_sidecar_port" mapstructure:"session_sidecar_port"`
	// GoogleHosts is the list of hosts where the sidecar injects the Google access token.
	GoogleHosts []string `json:"google_hosts" mapstructure:"google_hosts"`
	// GooglePaths is the list of paths where the sidecar injects the Google access token.
	GooglePaths []string `json:"google_paths" mapstructure:"google_paths"`
	// TodoistCredential is the scia credential ID used for Todoist OAuth, e.g. "takutakahashi.todoist".
	TodoistCredential string `json:"todoist_credential" mapstructure:"todoist_credential"`
	// TodoistHosts is the list of hosts where the sidecar injects the Todoist access token.
	TodoistHosts []string `json:"todoist_hosts" mapstructure:"todoist_hosts"`
	// TodoistPaths is the list of paths where the sidecar injects the Todoist access token.
	TodoistPaths []string `json:"todoist_paths" mapstructure:"todoist_paths"`
}

// DefaultKubernetesSessionImage is the immutable agent-assets revision used by
// Kubernetes session Pods. Application releases supply the ccplant CLI from
// the session-manager image independently.
const DefaultKubernetesSessionImage = "ghcr.io/ccplant/ccplant-agent:assets-a906c04a3e805ada770a51c2e4b50670"

const DefaultMoliImage = "ghcr.io/ccplant/ccplant-browser:moli-1.1.24-r1"
