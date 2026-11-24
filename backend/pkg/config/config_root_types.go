package config

// Config represents the proxy configuration
type Config struct {
	// BinaryPath is the ccplant executable used by generated hooks and child processes.
	BinaryPath string `json:"binary_path" mapstructure:"binary_path"`
	// Auth represents authentication configuration
	Auth AuthConfig `json:"auth" mapstructure:"auth"`
	// TeamDiscovery contains connection-aware GitHub team discovery rules.
	TeamDiscovery []TeamDiscoveryRule `json:"team_discovery" mapstructure:"team_discovery"`
	// AuthConfigFile is the path to an external auth configuration file (e.g., from ConfigMap)
	AuthConfigFile string `json:"auth_config_file" mapstructure:"auth_config_file"`
	// RoleEnvFiles is the configuration for role-based environment files
	RoleEnvFiles RoleEnvFilesConfig `json:"role_env_files" mapstructure:"role_env_files"`
	// KubernetesSession is the configuration for Kubernetes-based session management
	KubernetesSession KubernetesSessionConfig `json:"kubernetes_session" mapstructure:"kubernetes_session"`
	// ScheduleWorker is the configuration for the schedule worker
	ScheduleWorker ScheduleWorkerConfig `json:"schedule_worker" mapstructure:"schedule_worker"`
	// SlackbotCleanupWorker is the configuration for the Slackbot session cleanup worker
	SlackbotCleanupWorker SlackbotCleanupWorkerConfig `json:"slackbot_cleanup_worker" mapstructure:"slackbot_cleanup_worker"`
	// SessionRouteReconciler finalizes abandoned session routes and orphaned configurations.
	SessionRouteReconciler SessionRouteReconcilerConfig `json:"session_route_reconciler" mapstructure:"session_route_reconciler"`
	// StockInventoryWorker is the configuration for the stock session inventory worker.
	StockInventoryWorker StockInventoryWorkerConfig `json:"stock_inventory_worker" mapstructure:"stock_inventory_worker"`
	// Webhook is the configuration for webhook functionality
	Webhook WebhookConfig `json:"webhook" mapstructure:"webhook"`
	// Scia is the configuration for scia OAuth token broker/proxy integration.
	Scia               SciaConfig               `json:"scia" mapstructure:"scia"`
	SessionPersistence SessionPersistenceConfig `json:"session_persistence" mapstructure:"session_persistence"`
	// Slack is the configuration for Slack bot inbound webhook functionality
	Slack SlackConfig `json:"slack" mapstructure:"slack"`
	// SessionManager is the configuration for the session manager forwarding endpoint.
	SessionManager SessionManagerConfig `json:"session_manager" mapstructure:"session_manager"`
	// Worker is deliberately separate from KubernetesSession. A worker has no
	// Kubernetes workload credentials and talks to the API over this endpoint.
	Worker WorkerConfig `json:"worker" mapstructure:"worker"`
	// Redis holds optional Redis configuration for cross-pod status synchronisation.
	// Redis is required by the API and session manager runtime.
	Redis RedisConfig `json:"redis" mapstructure:"redis"`
	// KVStore controls storage for application KV data currently backed by
	// Kubernetes Secrets and ConfigMaps.
	KVStore KVStoreConfig `json:"kv_store" mapstructure:"kv_store"`
	// Usage controls response-level token usage collection.
	Usage UsageConfig `json:"usage" mapstructure:"usage"`
	// SessionCount controls pool/principal session count collection.
	SessionCount SessionCountConfig `json:"session_count" mapstructure:"session_count"`
	// SessionTokenDebug enables non-secret token-routing diagnostics for session creation.
	SessionTokenDebug bool `json:"session_token_debug" mapstructure:"session_token_debug"`
	// GitHubBrokerBaseURL overrides the request-derived base URL for session GitHub credentials.
	GitHubBrokerBaseURL string `json:"github_broker_base_url" mapstructure:"github_broker_base_url"`
	// CodexDeviceAuthCallbackBaseURL overrides the request-derived base URL that
	// short-lived Codex device auth workers use to report their challenge and
	// result. Workloads run inside the session cluster, so the browser-facing
	// host (which may sit behind an authentication proxy such as Cloudflare
	// Access) is not necessarily reachable from them. Defaults to the
	// request-derived URL when empty.
	CodexDeviceAuthCallbackBaseURL string `json:"codex_device_auth_callback_base_url" mapstructure:"codex_device_auth_callback_base_url"`
	ScheduleSessionTTL             string `json:"schedule_session_ttl" mapstructure:"schedule_session_ttl"`
	WebhookSessionTTL              string `json:"webhook_session_ttl" mapstructure:"webhook_session_ttl"`
	InteractiveSessionTTL          string `json:"interactive_session_ttl" mapstructure:"interactive_session_ttl"`
}

// SlackConfig represents Slack bot (Socket Mode) configuration
type SlackConfig struct {
	// AppToken and BotToken are write-only runtime inputs used by the dedicated
	// worker to materialize its default Socket Mode credential in the KV store.
	AppToken string `json:"app_token" mapstructure:"app_token"`
	BotToken string `json:"bot_token" mapstructure:"bot_token"`
	// AppTokenSecretName is the K8s Secret name containing the default App-level token (xapp-...).
	// Used for the default Socket Mode connection.
	// If empty, falls back to KubernetesSession.SlackBotTokenSecretName.
	// Set via AGENTAPI_SLACK_APP_TOKEN_SECRET_NAME environment variable.
	AppTokenSecretName string `json:"app_token_secret_name" mapstructure:"app_token_secret_name"`
	// AppTokenSecretKey is the key within the Secret for the App-level token.
	// Defaults to "app-token".
	// Set via AGENTAPI_SLACK_APP_TOKEN_SECRET_KEY environment variable.
	AppTokenSecretKey string `json:"app_token_secret_key" mapstructure:"app_token_secret_key"`
	// DryRun enables dry-run mode: session creation and Slack posts are logged but not executed.
	// Useful for testing event routing without side effects.
	// Set via AGENTAPI_SLACK_DRY_RUN environment variable.
	DryRun bool `json:"dry_run" mapstructure:"dry_run"`
}
