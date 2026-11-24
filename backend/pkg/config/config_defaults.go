package config

import (
	"log"

	"github.com/spf13/viper"
)

type configurationDefault struct {
	key   string
	value any
}

var configurationDefaults = []configurationDefault{
	{"binary_path", "ccplant"},
	// Auth defaults
	{"auth.bootstrap_admin.enabled", false},
	{"auth.bootstrap_admin.user_id", "bootstrap-admin"},
	{"auth.bootstrap_admin.username", "admin"},
	{"auth.bootstrap_admin.token", ""},
	{"auth.admin_key", ""},
	{"auth.github.enabled", false},
	{"auth.github.base_url", "https://api.github.com"},
	{"auth.github.token_header", "Authorization"},
	{"auth.github.oauth.client_id", ""},
	{"auth.github.oauth.client_secret", ""},
	{"auth.github.oauth.scope", "read:user read:org project"},
	{"auth.github.oauth.base_url", ""},

	// AWS auth defaults
	{"auth.aws.enabled", false},
	{"auth.aws.region", "ap-northeast-1"},

	{"auth.aws.team_tag_key", "Team"},
	{"auth.aws.cache_ttl", "1h"},
	{"usage.enabled", false},
	{"session_count.enabled", false},
	{"session_count.backend", "libsql"},

	// Role-based environment files defaults
	{"role_env_files.enabled", false},
	{"role_env_files.path", "/etc/agentapi/env"},
	{"role_env_files.load_default", true},

	// Kubernetes session defaults
	{"kubernetes_session.namespace", ""},
	{"kubernetes_session.image", ""},
	{"kubernetes_session.cli_image", ""},
	{"kubernetes_session.image_pull_policy", "IfNotPresent"},
	{"kubernetes_session.service_account", "agentapi-proxy-session"},
	{"kubernetes_session.base_port", 9000},
	{"kubernetes_session.cpu_request", "500m"},
	{"kubernetes_session.cpu_limit", "2"},
	{"kubernetes_session.memory_request", "512Mi"},
	{"kubernetes_session.memory_limit", "4Gi"},
	{"kubernetes_session.executor_enabled", true},
	{"kubernetes_session.executor_cpu_request", "10m"},
	{"kubernetes_session.executor_cpu_limit", "2"},
	{"kubernetes_session.executor_memory_request", "512Mi"},
	{"kubernetes_session.executor_memory_limit", "4Gi"},
	{"kubernetes_session.moli_enabled", false},
	{"kubernetes_session.moli_image", DefaultMoliImage},
	{"kubernetes_session.moli_port", 9222},
	{"kubernetes_session.moli_layout", true},
	{"kubernetes_session.moli_cpu_request", "100m"},
	{"kubernetes_session.moli_cpu_limit", "1"},
	{"kubernetes_session.moli_memory_request", "128Mi"},
	{"kubernetes_session.moli_memory_limit", "512Mi"},
	{"kubernetes_session.pvc_enabled", true},
	{"kubernetes_session.pvc_storage_class", ""},
	{"kubernetes_session.pvc_storage_size", "10Gi"},
	{"kubernetes_session.pod_start_timeout", 120},
	{"kubernetes_session.pod_stop_timeout", 30},
	{"kubernetes_session.provisioner_proxy_url", ""},
	{"kubernetes_session.network_filter_image", "ghcr.io/takutakahashi/nfa:0.12.6"},
	{"kubernetes_session.network_filter_cpu_request", "250m"},
	{"kubernetes_session.network_filter_cpu_limit", "1000m"},
	{"kubernetes_session.network_filter_memory_request", "256Mi"},
	{"kubernetes_session.network_filter_memory_limit", "512Mi"},
	{"kubernetes_session.network_filter_init_cpu_request", "50m"},
	{"kubernetes_session.network_filter_init_cpu_limit", "100m"},
	{"kubernetes_session.network_filter_init_memory_request", "32Mi"},
	{"kubernetes_session.network_filter_init_memory_limit", "64Mi"},
	{"kubernetes_session.github_secret_name", ""},
	{"worker.control_api_url", ""},
	{"worker.control_api_token", ""},
	{"worker.session_api_url", ""},
	{"session_manager.api_url", ""},
	{"session_manager.local_url", ""},
	{"session_manager.api_token", ""},
	{"session_manager.internal_api_token", ""},
	{"session_manager.allocation.lease_duration", "15s"},
	{"session_manager.allocation.renew_deadline", "10s"},
	{"session_manager.allocation.retry_period", "2s"},
	{"session_manager.allocation.lease_name", "agentapi-session-allocator"},
	{"session_manager.builtin.enabled", false},
	{"session_manager.builtin.manager_id", ""},
	{"session_manager.builtin.name", "Built-in session manager"},
	{"session_manager.builtin.connection_token", ""},
	{"session_manager.builtin.pool.name", ""},
	{"session_manager.builtin.pool.labels_json", ""},
	{"session_manager.builtin.pool.explicit_only", false},
	{"session_manager.builtin.pool.priority", -1},
	{"session_manager.builtin.pool.binding.subject_type", "all"},
	{"session_manager.builtin.pool.binding.role", "use"},

	// Settings base secret default (single base Secret shared by all sessions,
	// merged with team/user settings at session settings generation time)
	{"kubernetes_session.settings_base_secret", "agentapi-settings-base"},
	{"kubernetes_session.mcp_servers_base_secret", "mcp-servers-base"},

	// Schedule worker defaults
	{"schedule_worker.enabled", true},
	{"schedule_worker.check_interval", "30s"},
	{"schedule_worker.namespace", ""},
	{"schedule_worker.lease_duration", "9m"},
	{"schedule_worker.renew_deadline", "6m"},
	{"schedule_worker.retry_period", "1m"},

	// Slackbot cleanup worker defaults
	{"slackbot_cleanup_worker.enabled", false},
	{"slackbot_cleanup_worker.check_interval", "1h"},
	{"slackbot_cleanup_worker.session_ttl", "72h"},
	{"slackbot_cleanup_worker.session_ttl_check_interval", "1m"},
	{"slackbot_cleanup_worker.namespace", ""},
	{"slackbot_cleanup_worker.lease_duration", "9m"},
	{"slackbot_cleanup_worker.renew_deadline", "6m"},
	{"slackbot_cleanup_worker.retry_period", "1m"},

	// Session route reconciler defaults. Every rule acts only on definitive
	// signals (deleted manager/runner records, an unacknowledged deletion, the
	// runtime's own live status), so it is enabled by default. The
	// thresholds reclaim a stale session within about an hour.
	{"session_route_reconciler.enabled", true},
	{"session_route_reconciler.dry_run", false},
	{"session_route_reconciler.interval", "5m"},
	{"session_route_reconciler.orphan_grace", "15m"},
	{"session_route_reconciler.stuck_deletion_grace", "45m"},
	{"session_route_reconciler.runtime_disconnect_grace", "10m"},
	{"session_route_reconciler.configuration_orphan_grace", "45m"},

	// Stock inventory worker defaults
	{"stock_inventory_worker.enabled", false},
	{"stock_inventory_worker.check_interval", "30s"},
	{"stock_inventory_worker.target_count", 2},
	{"stock_inventory_worker.docker_enabled", false},
	{"stock_inventory_worker.namespace", ""},
	{"stock_inventory_worker.lease_duration", "9m"},
	{"stock_inventory_worker.renew_deadline", "6m"},
	{"stock_inventory_worker.retry_period", "1m"},

	// Webhook defaults
	{"webhook.base_url", ""},
	{"webhook.github_enterprise_host", ""},

	// scia defaults
	{"scia.enabled", false},
	{"scia.public_base_url", ""},
	{"scia.oauth_internal_url", ""},
	{"scia.proxy_url", ""},
	{"scia.credential", ""},
	{"scia.user_namespace", ""},
	{"scia.no_proxy", "localhost,127.0.0.1,.svc,.cluster.local"},
	{"scia.session_sidecar_enabled", false},
	{"scia.session_sidecar_image", "ghcr.io/takutakahashi/scia:0.17.0"},
	{"scia.session_sidecar_config_image", "busybox:1.36"},
	{"scia.session_sidecar_port", 18081},

	{"scia.todoist_credential", ""},

	{"session_persistence.backend", ""},
	{"session_persistence.path", "/var/lib/agentapi-session-state"},
	{"session_persistence.suspend_after", "1h"},
	{"session_persistence.s3.prefix", "agentapi-sessions/"},

	// Slack defaults
	{"slack.dry_run", false},

	// Redis defaults (empty addr = disabled)
	{"redis.addr", ""},
	{"redis.password", ""},
	{"redis.db", 0},
	{"redis.tls_enabled", false},
	{"redis.dial_timeout", "5s"},
	{"redis.read_timeout", "3s"},
	{"redis.write_timeout", "3s"},
	{"redis.session_control_pool_size", 50},
	{"redis.session_control_blocking_pool_size", 400},
}

func setDefaults(v *viper.Viper) {
	for _, setting := range configurationDefaults {
		v.SetDefault(setting.key, setting.value)
	}
	setCollectionDefaults(v)
}

// Give each Viper instance independent collection defaults.
func setCollectionDefaults(v *viper.Viper) {
	v.SetDefault("auth.aws.allowed_account_ids", []string{})
	v.SetDefault("scia.google_hosts", []string{"www.googleapis.com"})
	v.SetDefault("scia.google_paths", []string{"/calendar/v3/*"})
	v.SetDefault("scia.todoist_hosts", []string{"api.todoist.com"})
	v.SetDefault("scia.todoist_paths", []string{"/api/v1/*", "/rest/v2/*", "/sync/v9/*"})
}

// applyConfigDefaults applies default values to any unset configuration fields
func applyConfigDefaults(config *Config) {
	if config.KubernetesSession.BasePort == 0 {
		config.KubernetesSession.BasePort = 9000
	}

	// Apply auth defaults
	if config.Auth.BootstrapAdmin != nil {
		if config.Auth.BootstrapAdmin.UserID == "" {
			config.Auth.BootstrapAdmin.UserID = "bootstrap-admin"
		}
		if config.Auth.BootstrapAdmin.Username == "" {
			config.Auth.BootstrapAdmin.Username = "admin"
		}
	}
	if config.Auth.GitHub != nil {
		if config.Auth.GitHub.BaseURL == "" {
			config.Auth.GitHub.BaseURL = "https://api.github.com"
		}
		if config.Auth.GitHub.TokenHeader == "" {
			config.Auth.GitHub.TokenHeader = "Authorization"
		}
		if config.Auth.GitHub.OAuth != nil && config.Auth.GitHub.OAuth.Scope == "" {
			config.Auth.GitHub.OAuth.Scope = "read:user read:org project"
		}
	}
	if config.Scia.NoProxy == "" {
		config.Scia.NoProxy = "localhost,127.0.0.1,.svc,.cluster.local"
	}
	if config.Scia.SessionSidecarImage == "" {
		config.Scia.SessionSidecarImage = "ghcr.io/takutakahashi/scia:0.17.0"
	}
	if config.Scia.SessionSidecarConfigImage == "" {
		config.Scia.SessionSidecarConfigImage = "busybox:1.36"
	}
	if config.Scia.SessionSidecarPort == 0 {
		config.Scia.SessionSidecarPort = 18081
	}
	if len(config.Scia.GoogleHosts) == 0 {
		config.Scia.GoogleHosts = []string{"www.googleapis.com"}
	}
	if len(config.Scia.GooglePaths) == 0 {
		config.Scia.GooglePaths = []string{"/calendar/v3/*"}
	}
	if len(config.Scia.TodoistHosts) == 0 {
		config.Scia.TodoistHosts = []string{"api.todoist.com"}
	}
	if len(config.Scia.TodoistPaths) == 0 {
		config.Scia.TodoistPaths = []string{"/api/v1/*", "/rest/v2/*", "/sync/v9/*"}
	}
}

// postProcessConfig applies post-processing logic to the configuration
func postProcessConfig(config *Config) error {
	// Set default auth configuration
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		if config.Auth.GitHub.OAuth.BaseURL == "" {
			config.Auth.GitHub.OAuth.BaseURL = config.Auth.GitHub.BaseURL
		}
	}

	// Expand environment variables in OAuth configuration (for ${VAR_NAME} syntax in config files)
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		config.Auth.GitHub.OAuth.ClientID = expandEnvVars(config.Auth.GitHub.OAuth.ClientID)
		config.Auth.GitHub.OAuth.ClientSecret = expandEnvVars(config.Auth.GitHub.OAuth.ClientSecret)
	}

	// Log OAuth configuration status (after expansion)
	if config.Auth.GitHub != nil && config.Auth.GitHub.OAuth != nil {
		log.Printf("[CONFIG] OAuth ClientID configured: %v", config.Auth.GitHub.OAuth.ClientID != "")
		log.Printf("[CONFIG] OAuth ClientSecret configured: %v", config.Auth.GitHub.OAuth.ClientSecret != "")

		// Warn if OAuth is configured but credentials are missing
		if config.Auth.GitHub.OAuth.ClientID == "" || config.Auth.GitHub.OAuth.ClientSecret == "" {
			log.Printf("[CONFIG] Warning: OAuth is configured but Client ID or Client Secret is missing")
		}
	}

	// Log role-based environment files configuration
	if config.RoleEnvFiles.Enabled {
		log.Printf("[CONFIG] Role-based environment files enabled")
		log.Printf("[CONFIG] Environment files path: %s", config.RoleEnvFiles.Path)
		log.Printf("[CONFIG] Load default.env: %v", config.RoleEnvFiles.LoadDefault)
	}

	// Load kubernetes session config from external file if specified
	if config.KubernetesSession.ConfigFile != "" {
		if err := loadK8sSessionConfigFromFile(config, config.KubernetesSession.ConfigFile); err != nil {
			log.Printf("[CONFIG] Warning: Failed to load kubernetes session config from %s: %v", config.KubernetesSession.ConfigFile, err)
		} else {
			log.Printf("[CONFIG] Loaded kubernetes session config from: %s", config.KubernetesSession.ConfigFile)
		}
	}

	return nil
}
