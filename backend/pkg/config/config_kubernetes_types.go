package config

// Toleration represents a Kubernetes toleration for session pods
type Toleration struct {
	// Key is the taint key that the toleration applies to
	Key string `json:"key" mapstructure:"key" yaml:"key"`
	// Operator represents a key's relationship to the value (Equal or Exists)
	Operator string `json:"operator" mapstructure:"operator" yaml:"operator"`
	// Value is the taint value the toleration matches to
	Value string `json:"value" mapstructure:"value" yaml:"value"`
	// Effect indicates the taint effect to match (NoSchedule, PreferNoSchedule, NoExecute)
	Effect string `json:"effect" mapstructure:"effect" yaml:"effect"`
	// TolerationSeconds is the period of time the toleration tolerates the taint (for NoExecute)
	TolerationSeconds *int64 `json:"toleration_seconds,omitempty" mapstructure:"toleration_seconds" yaml:"toleration_seconds"`
}

// ScheduleWorkerConfig represents schedule worker configuration
type ScheduleWorkerConfig struct {
	// Enabled enables the schedule worker
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// CheckInterval is how often to check for due schedules (e.g., "30s", "1m")
	CheckInterval string `json:"check_interval" mapstructure:"check_interval"`
	// Namespace is the Kubernetes namespace for schedule resources
	Namespace string `json:"namespace" mapstructure:"namespace"`
	// LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership
	LeaseDuration string `json:"lease_duration" mapstructure:"lease_duration"`
	// RenewDeadline is the duration that the acting master will retry refreshing leadership before giving up
	RenewDeadline string `json:"renew_deadline" mapstructure:"renew_deadline"`
	// RetryPeriod is the duration the LeaderElector clients should wait between tries of actions
	RetryPeriod string `json:"retry_period" mapstructure:"retry_period"`
}

// SlackbotCleanupWorkerConfig represents Slackbot session cleanup worker configuration.
// The worker deletes Slackbot sessions whose processing ended more than SessionTTL ago.
type SlackbotCleanupWorkerConfig struct {
	// Enabled enables the Slackbot cleanup worker
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// CheckInterval is how often to scan for stale sessions (e.g., "1h", "30m")
	CheckInterval string `json:"check_interval" mapstructure:"check_interval"`
	// SessionTTL is the duration after processing ends before a session is deleted (e.g., "72h")
	SessionTTL string `json:"session_ttl" mapstructure:"session_ttl"`
	// SessionTTLCheckInterval is how often to scan for non-Slackbot sessions that have an
	// explicit agentapi.proxy/session-ttl annotation. This can be much shorter than
	// CheckInterval to support short-lived sessions. Default: "1m"
	SessionTTLCheckInterval string `json:"session_ttl_check_interval" mapstructure:"session_ttl_check_interval"`
	// DryRun disables actual deletion; stale sessions are only logged.
	// Useful for verifying TTL settings before enabling real cleanup.
	DryRun bool `json:"dry_run" mapstructure:"dry_run"`
	// LeaseDuration is the duration that non-leader candidates will wait to force acquire leadership
	LeaseDuration string `json:"lease_duration" mapstructure:"lease_duration"`
	// RenewDeadline is the duration that the acting master will retry refreshing leadership before giving up
	RenewDeadline string `json:"renew_deadline" mapstructure:"renew_deadline"`
	// RetryPeriod is the duration the LeaderElector clients should wait between tries of actions
	RetryPeriod string `json:"retry_period" mapstructure:"retry_period"`
}

// SessionRouteReconcilerConfig configures the API-side reconciler that
// finalizes session routes whose owning manager or runner is gone and garbage
// collects session configurations that no longer belong to any session.
type SessionRouteReconcilerConfig struct {
	// Enabled starts the reconciler on the elected API replica.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// DryRun only logs what would be finalized.
	DryRun bool `json:"dry_run" mapstructure:"dry_run"`
	// Interval between reconciliation passes (e.g. "5m").
	Interval string `json:"interval" mapstructure:"interval"`
	// OrphanGrace protects routes whose status changed recently (e.g. "15m").
	OrphanGrace string `json:"orphan_grace" mapstructure:"orphan_grace"`
	// StuckDeletionGrace finalizes routes stuck terminating/delete_failed (e.g. "45m").
	StuckDeletionGrace string `json:"stuck_deletion_grace" mapstructure:"stuck_deletion_grace"`
	// RuntimeDisconnectGrace reclaims a TTL session whose runtime stayed disconnected this long (e.g. "10m").
	RuntimeDisconnectGrace string `json:"runtime_disconnect_grace" mapstructure:"runtime_disconnect_grace"`
	// ConfigurationOrphanGrace deletes unowned configurations idle for this long (e.g. "45m").
	ConfigurationOrphanGrace string `json:"configuration_orphan_grace" mapstructure:"configuration_orphan_grace"`
}

// StockInventoryWorkerConfig represents stock inventory worker configuration.
// The worker ensures a target number of pre-warmed stock sessions are always available.
// Note: Sandbox (network filter) and scia sidecar are now always enabled.
type StockInventoryWorkerConfig struct {
	// Enabled controls whether the worker runs. Default: false (opt-in).
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// CheckInterval is how often to check and replenish stock sessions. Default: "30s".
	CheckInterval string `json:"check_interval" mapstructure:"check_interval"`
	// TargetCount is the desired number of stock sessions to maintain. Default: 2.
	TargetCount int `json:"target_count" mapstructure:"target_count"`
	// DockerEnabled controls whether stock sessions include the Docker-in-Docker sidecar.
	DockerEnabled bool `json:"docker_enabled" mapstructure:"docker_enabled"`
	// Pools optionally configures multiple stock pools. When set, each pool is
	// replenished independently; otherwise the legacy single-pool fields above
	// are used.
	Pools []StockInventoryPoolConfig `json:"pools" mapstructure:"pools"`
	// Namespace overrides the Kubernetes namespace (falls back to KubernetesSession.Namespace).
	Namespace string `json:"namespace" mapstructure:"namespace"`
	// Leader election timings.
	LeaseDuration string `json:"lease_duration" mapstructure:"lease_duration"`
	RenewDeadline string `json:"renew_deadline" mapstructure:"renew_deadline"`
	RetryPeriod   string `json:"retry_period" mapstructure:"retry_period"`
}

// StockInventoryPoolConfig represents one stock inventory capability pool.
// Note: Sandbox (network filter) and scia sidecar are now always enabled.
// Only DockerEnabled remains configurable.
type StockInventoryPoolConfig struct {
	// Name is the logical session pool claimed directly by idle runners.
	Name string `json:"name" mapstructure:"name"`
	// TargetCount is the desired number of stock sessions for this capability pool.
	TargetCount int `json:"target_count" mapstructure:"target_count"`
	// DockerEnabled controls whether stock sessions include the Docker-in-Docker sidecar.
	DockerEnabled bool `json:"docker_enabled" mapstructure:"docker_enabled"`
}
