package config

import (
	"fmt"
	"github.com/spf13/viper"
)

// configurationEnvBindings declares nested keys and ordered environment aliases.
var configurationEnvBindings = [][]string{
	{"binary_path", "CCPLANT_BINARY_PATH"},

	// Auth configuration
	{"auth.admin_key"},
	{"auth.bootstrap_admin.enabled"},
	{"auth.bootstrap_admin.user_id"},
	{"auth.bootstrap_admin.username"},
	{"auth.bootstrap_admin.token"},

	// GitHub auth configuration
	{"auth.github.enabled"},
	{"auth.github.connection_id"},
	{"auth.github.base_url"},
	{"auth.github.token_header"},
	{"auth.github.user_mapping.default_role"},
	{"auth.github.user_mapping.default_permissions"},
	{"auth.github.user_mapping.allow_users_without_team"},

	// GitHub OAuth configuration
	{"auth.github.oauth.client_id"},
	{"auth.github.oauth.client_secret"},
	{"auth.github.oauth.scope"},
	{"auth.github.oauth.base_url"},

	// Other configuration
	{"auth_config_file"},
	{"team_discovery", "AGENTAPI_TEAM_DISCOVERY"},
	{"session_token_debug", "AGENTAPI_SESSION_TOKEN_DEBUG_ENABLED"},
	{"github_broker_base_url", "AGENTAPI_GITHUB_BROKER_BASE_URL"},
	{"schedule_session_ttl", "AGENTAPI_SCHEDULE_SESSION_TTL"},
	{"webhook_session_ttl", "AGENTAPI_WEBHOOK_SESSION_TTL"},
	{"interactive_session_ttl", "AGENTAPI_INTERACTIVE_SESSION_TTL"},
	{"codex_device_auth_callback_base_url", "AGENTAPI_CODEX_DEVICE_AUTH_CALLBACK_BASE_URL"},
	{"kv_store.backend", "AGENTAPI_KV_STORE_BACKEND"},
	{"kv_store.namespace", "AGENTAPI_KV_STORE_NAMESPACE"},
	{"kv_store.database_url", "AGENTAPI_KV_STORE_DATABASE_URL"},
	{"kv_store.auth_token", "AGENTAPI_KV_STORE_AUTH_TOKEN"},
	{"kv_store.primary.backend", "AGENTAPI_KV_STORE_PRIMARY_BACKEND"},
	{"kv_store.primary.database_url", "AGENTAPI_KV_STORE_PRIMARY_DATABASE_URL"},
	{"kv_store.primary.auth_token", "AGENTAPI_KV_STORE_PRIMARY_AUTH_TOKEN"},
	{"kv_store.primary.encryption.active_key_id", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_ACTIVE_KEY_ID"},
	{"kv_store.primary.encryption.provider", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_PROVIDER"},
	{"kv_store.primary.encryption.kms_key_id", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KMS_KEY_ID"},
	{"kv_store.primary.encryption.kms_region", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KMS_REGION"},
	{"kv_store.primary.encryption.keys", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_KEYS"},
	{"kv_store.primary.encryption.branch_cache_ttl_seconds", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_BRANCH_CACHE_TTL_SECONDS"},
	{"kv_store.primary.encryption.branch_cache_max_entries", "AGENTAPI_KV_STORE_PRIMARY_ENCRYPTION_BRANCH_CACHE_MAX_ENTRIES"},
	{"kv_store.secondary.backend", "AGENTAPI_KV_STORE_SECONDARY_BACKEND"},
	{"kv_store.secondary.database_url", "AGENTAPI_KV_STORE_SECONDARY_DATABASE_URL"},
	{"kv_store.secondary.auth_token", "AGENTAPI_KV_STORE_SECONDARY_AUTH_TOKEN"},
	{"kv_store.secondary.encryption.active_key_id", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_ACTIVE_KEY_ID"},
	{"kv_store.secondary.encryption.provider", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_PROVIDER"},
	{"kv_store.secondary.encryption.kms_key_id", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KMS_KEY_ID"},
	{"kv_store.secondary.encryption.kms_region", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KMS_REGION"},
	{"kv_store.secondary.encryption.keys", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_KEYS"},
	{"kv_store.secondary.encryption.branch_cache_ttl_seconds", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_BRANCH_CACHE_TTL_SECONDS"},
	{"kv_store.secondary.encryption.branch_cache_max_entries", "AGENTAPI_KV_STORE_SECONDARY_ENCRYPTION_BRANCH_CACHE_MAX_ENTRIES"},
	{"kv_store.replication.mode", "AGENTAPI_KV_STORE_REPLICATION_MODE"},
	{"kv_store.encryption.active_key_id", "AGENTAPI_KV_ENCRYPTION_ACTIVE_KEY_ID"},
	{"kv_store.encryption.provider", "AGENTAPI_KV_ENCRYPTION_PROVIDER"},
	{"kv_store.encryption.kms_region", "AGENTAPI_KV_ENCRYPTION_KMS_REGION"},
	{"kv_store.encryption.keys", "AGENTAPI_KV_ENCRYPTION_KEYS"},
	{"kv_store.encryption.branch_cache_ttl_seconds", "AGENTAPI_KV_ENCRYPTION_BRANCH_CACHE_TTL_SECONDS"},
	{"kv_store.encryption.branch_cache_max_entries", "AGENTAPI_KV_ENCRYPTION_BRANCH_CACHE_MAX_ENTRIES"},
	{"usage.enabled", "AGENTAPI_USAGE_ENABLED"},
	{"usage.database_url", "AGENTAPI_USAGE_DATABASE_URL"},
	{"usage.auth_token", "AGENTAPI_USAGE_AUTH_TOKEN"},
	{"session_count.enabled", "AGENTAPI_SESSION_COUNT_ENABLED"},
	{"session_count.backend", "AGENTAPI_SESSION_COUNT_BACKEND"},
	{"session_count.database_url", "AGENTAPI_SESSION_COUNT_DATABASE_URL"},
	{"session_count.auth_token", "AGENTAPI_SESSION_COUNT_AUTH_TOKEN"},

	// scia OAuth broker/proxy configuration
	{"scia.enabled", "AGENTAPI_SCIA_ENABLED"},
	{"scia.public_base_url", "AGENTAPI_SCIA_PUBLIC_BASE_URL"},
	{"scia.oauth_internal_url", "AGENTAPI_SCIA_OAUTH_INTERNAL_URL"},
	{"scia.proxy_url", "AGENTAPI_SCIA_PROXY_URL"},
	{"scia.credential", "AGENTAPI_SCIA_CREDENTIAL"},
	{"scia.user_namespace", "AGENTAPI_SCIA_USER_NAMESPACE"},
	{"scia.no_proxy", "AGENTAPI_SCIA_NO_PROXY"},
	{"scia.session_sidecar_enabled", "AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED"},
	{"scia.session_sidecar_image", "AGENTAPI_SCIA_SESSION_SIDECAR_IMAGE"},
	{"scia.session_sidecar_config_image", "AGENTAPI_SCIA_SESSION_SIDECAR_CONFIG_IMAGE"},
	{"scia.session_sidecar_port", "AGENTAPI_SCIA_SESSION_SIDECAR_PORT"},
	{"scia.google_hosts", "AGENTAPI_SCIA_GOOGLE_HOSTS"},
	{"scia.google_paths", "AGENTAPI_SCIA_GOOGLE_PATHS"},
	{"scia.todoist_credential", "AGENTAPI_SCIA_TODOIST_CREDENTIAL"},
	{"scia.todoist_hosts", "AGENTAPI_SCIA_TODOIST_HOSTS"},
	{"scia.todoist_paths", "AGENTAPI_SCIA_TODOIST_PATHS"},

	// Role-based environment files configuration
	{"role_env_files.enabled"},
	{"role_env_files.path"},
	{"role_env_files.load_default"},

	// Kubernetes session configuration
	{"kubernetes_session.namespace", "AGENTAPI_K8S_SESSION_NAMESPACE"},
	{"kubernetes_session.image", "AGENTAPI_K8S_SESSION_IMAGE"},
	{"kubernetes_session.cli_image", "AGENTAPI_K8S_SESSION_CLI_IMAGE"},
	{"kubernetes_session.image_pull_policy", "AGENTAPI_K8S_SESSION_IMAGE_PULL_POLICY"},
	{"kubernetes_session.service_account", "AGENTAPI_K8S_SESSION_SERVICE_ACCOUNT"},
	{"kubernetes_session.disable_service_links", "AGENTAPI_K8S_SESSION_DISABLE_SERVICE_LINKS"},
	{"kubernetes_session.disable_service_account_token", "AGENTAPI_K8S_SESSION_DISABLE_SERVICE_ACCOUNT_TOKEN"},
	{"kubernetes_session.base_port", "AGENTAPI_K8S_SESSION_BASE_PORT"},
	{"kubernetes_session.cpu_request", "AGENTAPI_K8S_SESSION_CPU_REQUEST"},
	{"kubernetes_session.cpu_limit", "AGENTAPI_K8S_SESSION_CPU_LIMIT"},
	{"kubernetes_session.memory_request", "AGENTAPI_K8S_SESSION_MEMORY_REQUEST"},
	{"kubernetes_session.memory_limit", "AGENTAPI_K8S_SESSION_MEMORY_LIMIT"},
	{"kubernetes_session.executor_enabled", "AGENTAPI_K8S_SESSION_EXECUTOR_ENABLED"},
	{"kubernetes_session.executor_cpu_request", "AGENTAPI_K8S_SESSION_EXECUTOR_CPU_REQUEST"},
	{"kubernetes_session.executor_cpu_limit", "AGENTAPI_K8S_SESSION_EXECUTOR_CPU_LIMIT"},
	{"kubernetes_session.executor_memory_request", "AGENTAPI_K8S_SESSION_EXECUTOR_MEMORY_REQUEST"},
	{"kubernetes_session.executor_memory_limit", "AGENTAPI_K8S_SESSION_EXECUTOR_MEMORY_LIMIT"},
	{"kubernetes_session.moli_enabled", "AGENTAPI_K8S_SESSION_MOLI_ENABLED"},
	{"kubernetes_session.moli_image", "AGENTAPI_K8S_SESSION_MOLI_IMAGE"},
	{"kubernetes_session.moli_port", "AGENTAPI_K8S_SESSION_MOLI_PORT"},
	{"kubernetes_session.moli_layout", "AGENTAPI_K8S_SESSION_MOLI_LAYOUT"},
	{"kubernetes_session.moli_cpu_request", "AGENTAPI_K8S_SESSION_MOLI_CPU_REQUEST"},
	{"kubernetes_session.moli_cpu_limit", "AGENTAPI_K8S_SESSION_MOLI_CPU_LIMIT"},
	{"kubernetes_session.moli_memory_request", "AGENTAPI_K8S_SESSION_MOLI_MEMORY_REQUEST"},
	{"kubernetes_session.moli_memory_limit", "AGENTAPI_K8S_SESSION_MOLI_MEMORY_LIMIT"},
	{"kubernetes_session.pvc_enabled", "AGENTAPI_K8S_SESSION_PVC_ENABLED"},
	{"kubernetes_session.pvc_storage_class", "AGENTAPI_K8S_SESSION_PVC_STORAGE_CLASS"},
	{"kubernetes_session.pvc_storage_size", "AGENTAPI_K8S_SESSION_PVC_STORAGE_SIZE"},
	{"kubernetes_session.pod_start_timeout", "AGENTAPI_K8S_SESSION_POD_START_TIMEOUT"},
	{"kubernetes_session.pod_stop_timeout", "AGENTAPI_K8S_SESSION_POD_STOP_TIMEOUT"},
	{"kubernetes_session.provisioner_proxy_url", "AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL"},
	{"kubernetes_session.provisioner_token", "AGENTAPI_K8S_SESSION_PROVISIONER_TOKEN"},
	{"kubernetes_session.network_filter_image", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_IMAGE"},
	{"kubernetes_session.network_filter_cpu_request", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_CPU_REQUEST"},
	{"kubernetes_session.network_filter_cpu_limit", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_CPU_LIMIT"},
	{"kubernetes_session.network_filter_memory_request", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_MEMORY_REQUEST"},
	{"kubernetes_session.network_filter_memory_limit", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_MEMORY_LIMIT"},
	{"kubernetes_session.network_filter_init_cpu_request", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_INIT_CPU_REQUEST"},
	{"kubernetes_session.network_filter_init_cpu_limit", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_INIT_CPU_LIMIT"},
	{"kubernetes_session.network_filter_init_memory_request", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_INIT_MEMORY_REQUEST"},
	{"kubernetes_session.network_filter_init_memory_limit", "AGENTAPI_K8S_SESSION_NETWORK_FILTER_INIT_MEMORY_LIMIT"},
	{"kubernetes_session.github_secret_name", "AGENTAPI_K8S_SESSION_GITHUB_SECRET_NAME"},
	{"kubernetes_session.github_config_secret_name", "AGENTAPI_K8S_SESSION_GITHUB_CONFIG_SECRET_NAME"},
	{"kubernetes_session.config_file", "AGENTAPI_K8S_SESSION_CONFIG_FILE"},
	{"kubernetes_session.session_pod_template_file", "AGENTAPI_K8S_SESSION_POD_TEMPLATE_FILE"},
	// MCP servers configuration

	// Settings base secret configuration
	{"kubernetes_session.settings_base_secret", "AGENTAPI_K8S_SESSION_SETTINGS_BASE_SECRET"},
	{"kubernetes_session.mcp_servers_base_secret", "AGENTAPI_K8S_SESSION_MCP_SERVERS_BASE_SECRET"},
	{"kubernetes_session.mcp_servers_base_file", "AGENTAPI_K8S_SESSION_MCP_SERVERS_BASE_FILE"},

	// OpenTelemetry Collector configuration
	{"kubernetes_session.otel_collector_enabled", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_ENABLED"},
	{"kubernetes_session.otel_collector_scrape_interval", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_SCRAPE_INTERVAL"},
	{"kubernetes_session.otel_collector_claude_code_port", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_CLAUDE_CODE_PORT"},
	{"kubernetes_session.otel_collector_exporter_port", "AGENTAPI_KUBERNETES_SESSION_OTEL_COLLECTOR_EXPORTER_PORT"},

	// Slack Integration configuration
	{"kubernetes_session.slack_bot_token_secret_name", "AGENTAPI_KUBERNETES_SESSION_SLACK_BOT_TOKEN_SECRET_NAME"},
	{"kubernetes_session.slack_bot_token_secret_key", "AGENTAPI_KUBERNETES_SESSION_SLACK_BOT_TOKEN_SECRET_KEY"},

	// Schedule worker configuration
	{"schedule_worker.enabled", "AGENTAPI_SCHEDULE_WORKER_ENABLED"},
	{"schedule_worker.check_interval", "AGENTAPI_SCHEDULE_WORKER_CHECK_INTERVAL"},
	{"schedule_worker.namespace", "AGENTAPI_SCHEDULE_WORKER_NAMESPACE"},
	{"schedule_worker.lease_duration", "AGENTAPI_SCHEDULE_WORKER_LEASE_DURATION"},
	{"schedule_worker.renew_deadline", "AGENTAPI_SCHEDULE_WORKER_RENEW_DEADLINE"},
	{"schedule_worker.retry_period", "AGENTAPI_SCHEDULE_WORKER_RETRY_PERIOD"},

	// Slackbot cleanup worker configuration
	{"slackbot_cleanup_worker.enabled", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_ENABLED"},
	{"slackbot_cleanup_worker.check_interval", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_CHECK_INTERVAL"},
	{"slackbot_cleanup_worker.session_ttl", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_SESSION_TTL"},
	{"slackbot_cleanup_worker.session_ttl_check_interval", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_SESSION_TTL_CHECK_INTERVAL"},
	{"slackbot_cleanup_worker.dry_run", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_DRY_RUN"},
	{"slackbot_cleanup_worker.lease_duration", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_LEASE_DURATION"},
	{"slackbot_cleanup_worker.renew_deadline", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_RENEW_DEADLINE"},
	{"slackbot_cleanup_worker.retry_period", "AGENTAPI_SLACKBOT_CLEANUP_WORKER_RETRY_PERIOD"},

	// Session route reconciler configuration
	{"session_route_reconciler.enabled", "AGENTAPI_SESSION_ROUTE_RECONCILER_ENABLED"},
	{"session_route_reconciler.dry_run", "AGENTAPI_SESSION_ROUTE_RECONCILER_DRY_RUN"},
	{"session_route_reconciler.interval", "AGENTAPI_SESSION_ROUTE_RECONCILER_INTERVAL"},
	{"session_route_reconciler.orphan_grace", "AGENTAPI_SESSION_ROUTE_RECONCILER_ORPHAN_GRACE"},
	{"session_route_reconciler.stuck_deletion_grace", "AGENTAPI_SESSION_ROUTE_RECONCILER_STUCK_DELETION_GRACE"},
	{"session_route_reconciler.runtime_disconnect_grace", "AGENTAPI_SESSION_ROUTE_RECONCILER_RUNTIME_DISCONNECT_GRACE"},
	{"session_route_reconciler.configuration_orphan_grace", "AGENTAPI_SESSION_ROUTE_RECONCILER_CONFIGURATION_ORPHAN_GRACE"},

	// Stock inventory worker configuration
	{"stock_inventory_worker.enabled", "AGENTAPI_STOCK_INVENTORY_WORKER_ENABLED"},
	{"stock_inventory_worker.check_interval", "AGENTAPI_STOCK_INVENTORY_WORKER_CHECK_INTERVAL"},
	{"stock_inventory_worker.target_count", "AGENTAPI_STOCK_INVENTORY_WORKER_TARGET_COUNT"},
	{"stock_inventory_worker.docker_enabled", "AGENTAPI_STOCK_INVENTORY_WORKER_DOCKER_ENABLED"},
	{"stock_inventory_worker.pools", "AGENTAPI_STOCK_INVENTORY_WORKER_POOLS"},
	{"stock_inventory_worker.namespace", "AGENTAPI_STOCK_INVENTORY_WORKER_NAMESPACE"},
	{"stock_inventory_worker.lease_duration", "AGENTAPI_STOCK_INVENTORY_WORKER_LEASE_DURATION"},
	{"stock_inventory_worker.renew_deadline", "AGENTAPI_STOCK_INVENTORY_WORKER_RENEW_DEADLINE"},
	{"stock_inventory_worker.retry_period", "AGENTAPI_STOCK_INVENTORY_WORKER_RETRY_PERIOD"},

	// Webhook configuration
	{"webhook.base_url", "AGENTAPI_WEBHOOK_BASE_URL"},
	{"webhook.github_enterprise_host", "AGENTAPI_WEBHOOK_GITHUB_ENTERPRISE_HOST"},

	// Slack configuration
	{"slack.app_token_secret_name", "AGENTAPI_SLACK_APP_TOKEN_SECRET_NAME"},
	{"slack.app_token_secret_key", "AGENTAPI_SLACK_APP_TOKEN_SECRET_KEY"},
	{"slack.app_token", "AGENTAPI_SLACK_APP_TOKEN"},
	{"slack.bot_token", "AGENTAPI_SLACK_BOT_TOKEN"},
	{"slack.dry_run", "AGENTAPI_SLACK_DRY_RUN"},

	// Session manager configuration
	{"session_manager.enabled", "AGENTAPI_SESSION_MANAGER_ENABLED", "SESSION_MANAGER_ENABLED"},
	{"session_manager.hmac_secret", "AGENTAPI_SESSION_MANAGER_HMAC_SECRET", "SESSION_MANAGER_HMAC_SECRET"},
	{"session_manager.upstream_url", "AGENTAPI_SESSION_MANAGER_UPSTREAM_URL", "SESSION_MANAGER_UPSTREAM_URL"},
	{"session_manager.connection_token", "AGENTAPI_SESSION_MANAGER_CONNECTION_TOKEN", "SESSION_MANAGER_CONNECTION_TOKEN"},
	{"session_manager.id", "AGENTAPI_SESSION_MANAGER_ID", "SESSION_MANAGER_ID"},
	{"session_manager.runner_pool", "AGENTAPI_SESSION_MANAGER_RUNNER_POOL", "SESSION_MANAGER_RUNNER_POOL"},
	{"session_manager.api_url", "AGENTAPI_SESSION_MANAGER_API_URL"},
	{"session_manager.local_url", "AGENTAPI_SESSION_MANAGER_LOCAL_URL"},
	{"session_manager.api_token", "AGENTAPI_SESSION_MANAGER_API_TOKEN"},
	{"session_manager.internal_api_token", "AGENTAPI_SESSION_MANAGER_INTERNAL_API_TOKEN"},
	{"session_manager.auto_upgrade", "AGENTAPI_SESSION_MANAGER_AUTO_UPGRADE"},
	{"session_manager.upgrade_version_url", "AGENTAPI_SESSION_MANAGER_UPGRADE_VERSION_URL"},
	{"session_manager.deployment_name", "AGENTAPI_SESSION_MANAGER_DEPLOYMENT_NAME"},
	{"session_manager.image_repository", "AGENTAPI_SESSION_MANAGER_IMAGE_REPOSITORY"},
	{"session_manager.current_version", "AGENTAPI_SESSION_MANAGER_CURRENT_VERSION"},
	{"session_manager.allocation.lease_duration", "AGENTAPI_SESSION_MANAGER_ALLOCATION_LEASE_DURATION"},
	{"session_manager.allocation.renew_deadline", "AGENTAPI_SESSION_MANAGER_ALLOCATION_RENEW_DEADLINE"},
	{"session_manager.allocation.retry_period", "AGENTAPI_SESSION_MANAGER_ALLOCATION_RETRY_PERIOD"},
	{"session_manager.allocation.lease_name", "AGENTAPI_SESSION_MANAGER_ALLOCATION_LEASE_NAME"},
	{"session_manager.builtin.enabled", "AGENTAPI_SESSION_MANAGER_BUILTIN_ENABLED"},
	{"session_manager.builtin.manager_id", "AGENTAPI_SESSION_MANAGER_BUILTIN_MANAGER_ID"},
	{"session_manager.builtin.name", "AGENTAPI_SESSION_MANAGER_BUILTIN_NAME"},
	{"session_manager.builtin.connection_token", "AGENTAPI_SESSION_MANAGER_BUILTIN_CONNECTION_TOKEN"},
	{"session_manager.builtin.pool.name", "AGENTAPI_SESSION_MANAGER_BUILTIN_POOL"},
	{"session_manager.builtin.pool.explicit_only", "AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_EXPLICIT_ONLY"},
	{"session_manager.builtin.pool.priority", "AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_PRIORITY"},
	{"session_manager.builtin.pool.labels_json", "AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_LABELS"},
	{"session_manager.builtin.pool.binding.subject_type", "AGENTAPI_SESSION_MANAGER_BUILTIN_BINDING_SUBJECT_TYPE"},
	{"session_manager.builtin.pool.binding.role", "AGENTAPI_SESSION_MANAGER_BUILTIN_BINDING_ROLE"},

	// Background-worker control plane. These values are deliberately not part
	// of kubernetes_session: the worker must never receive a provisioner token.
	{"worker.control_api_url", "AGENTAPI_WORKER_CONTROL_API_URL", "AGENTAPI_K8S_SESSION_PROVISIONER_PROXY_URL"},
	{"worker.control_api_token", "AGENTAPI_WORKER_CONTROL_TOKEN"},
	{"worker.execution_token_signing_key", "AGENTAPI_EXECUTION_TOKEN_SIGNING_KEY"},
	{"worker.session_api_url", "AGENTAPI_WORKER_SESSION_API_URL"},

	{"session_persistence.backend", "AGENTAPI_SESSION_PERSISTENCE_BACKEND"},
	{"session_persistence.path", "AGENTAPI_SESSION_PERSISTENCE_PATH"},
	{"session_persistence.suspend_after", "AGENTAPI_SESSION_PERSISTENCE_SUSPEND_AFTER"},
	{"session_persistence.s3.bucket", "AGENTAPI_SESSION_PERSISTENCE_S3_BUCKET"},
	{"session_persistence.s3.region", "AGENTAPI_SESSION_PERSISTENCE_S3_REGION"},
	{"session_persistence.s3.prefix", "AGENTAPI_SESSION_PERSISTENCE_S3_PREFIX"},
	{"session_persistence.s3.endpoint", "AGENTAPI_SESSION_PERSISTENCE_S3_ENDPOINT"},
	{"session_persistence.s3.access_key_id", "AGENTAPI_SESSION_PERSISTENCE_S3_ACCESS_KEY_ID"},
	{"session_persistence.s3.secret_access_key", "AGENTAPI_SESSION_PERSISTENCE_S3_SECRET_ACCESS_KEY"},

	// External memory-server backend configuration

	// Redis configuration
	{"redis.addr", "AGENTAPI_REDIS_ADDR"},
	{"redis.password", "AGENTAPI_REDIS_PASSWORD"},
	{"redis.db", "AGENTAPI_REDIS_DB"},
	{"redis.tls_enabled", "AGENTAPI_REDIS_TLS_ENABLED"},
	{"redis.dial_timeout", "AGENTAPI_REDIS_DIAL_TIMEOUT"},
	{"redis.read_timeout", "AGENTAPI_REDIS_READ_TIMEOUT"},
	{"redis.write_timeout", "AGENTAPI_REDIS_WRITE_TIMEOUT"},
	{"redis.session_control_pool_size", "AGENTAPI_REDIS_SESSION_CONTROL_POOL_SIZE"},
	{"redis.session_control_blocking_pool_size", "AGENTAPI_REDIS_SESSION_CONTROL_BLOCKING_POOL_SIZE"},
}

func bindEnvVars(v *viper.Viper) error {
	for _, binding := range configurationEnvBindings {
		if err := v.BindEnv(binding...); err != nil {
			return fmt.Errorf("bind configuration environment: %w", err)
		}
	}
	return nil
}
