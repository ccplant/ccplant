package config

// S3StorageConfig represents shared S3 storage configuration.
type S3StorageConfig struct {
	// Bucket is the S3 bucket name (required)
	Bucket string `json:"bucket" mapstructure:"bucket"`
	// Region is the AWS region (optional, uses AWS default config if empty)
	Region string `json:"region" mapstructure:"region"`
	// Prefix is the key prefix for stored objects.
	Prefix string `json:"prefix" mapstructure:"prefix"`
	// Endpoint is a custom S3-compatible endpoint URL (e.g., for rustfs or other S3-compatible services)
	Endpoint string `json:"endpoint" mapstructure:"endpoint"`
	// AccessKeyID and SecretAccessKey optionally isolate S3-compatible storage
	// credentials from the process-wide AWS credential chain (for example KMS).
	AccessKeyID     string `json:"access_key_id" mapstructure:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key" mapstructure:"secret_access_key"`
}

// SessionPersistenceConfig stores ACP conversation snapshots. "volume" writes
// to each Kubernetes session's workdir PVC; "s3" uses an S3-compatible service.
type SessionPersistenceConfig struct {
	Backend      string           `json:"backend" mapstructure:"backend"`
	Path         string           `json:"path" mapstructure:"path"`
	SuspendAfter string           `json:"suspend_after" mapstructure:"suspend_after"`
	S3           *S3StorageConfig `json:"s3,omitempty" mapstructure:"s3"`
}

// SessionManagerConfig holds configuration for the session manager forwarding endpoint.
// When enabled, External Session Manager (small-cluster mode) accepts pre-built SessionSettings from a
// trusted upstream proxy (親プロキシ) and creates sessions without requiring local secrets.
type SessionManagerConfig struct {
	// Enabled enables the /api/v1/sessions forwarding endpoint.
	Enabled bool `json:"enabled" mapstructure:"enabled"`
	// HMACSecret is the shared HMAC-SHA256 secret used to verify request signatures.
	// Every inbound request must carry X-Hub-Signature-256: sha256=<hex> computed
	// over the raw request body with this secret.
	// Can also be set via SESSION_MANAGER_HMAC_SECRET environment variable.
	HMACSecret string `json:"hmac_secret" mapstructure:"hmac_secret"`
	// UpstreamURL is the 親プロキシ URL to poll for external session allocations.
	// Can also be set via SESSION_MANAGER_UPSTREAM_URL.
	UpstreamURL string `json:"upstream_url" mapstructure:"upstream_url"`
	// ConnectionToken authenticates this manager to 親プロキシ's allocator endpoint.
	// Can also be set via SESSION_MANAGER_CONNECTION_TOKEN.
	ConnectionToken string `json:"connection_token" mapstructure:"connection_token"`
	// ID identifies this manager in the cluster-wide registry.
	ID string `json:"id" mapstructure:"id"`
	// RunnerPool enables GitHub Actions-style runner claim mode for stock pods.
	RunnerPool string `json:"runner_pool" mapstructure:"runner_pool"`
	// APIURL is the private peer API URL. The public API process uses it to call
	// the isolated session manager, while an external session-manager process
	// uses it as the local target for outbound control-tunnel requests.
	// It is intentionally distinct from UpstreamURL, which identifies the parent
	// control plane.
	APIURL string `json:"api_url" mapstructure:"api_url"`
	// LocalURL is the in-cluster URL of this session-manager process. Session
	// Pods use it even when APIURL or the runner upstream targets another plane.
	LocalURL string `json:"local_url" mapstructure:"local_url"`
	// APIToken authenticates outbound API -> session-manager requests.
	APIToken string `json:"api_token" mapstructure:"api_token"`
	// InternalAPIToken authenticates inbound requests to the private manager API.
	InternalAPIToken string `json:"internal_api_token" mapstructure:"internal_api_token"`
	// AutoUpgrade lets a Kubernetes manager roll its own Deployment forward to
	// the version advertised by the connected parent proxy.
	AutoUpgrade bool `json:"auto_upgrade" mapstructure:"auto_upgrade"`
	// UpgradeVersionURL is an optional HTTP endpoint polled by a standalone
	// Kubernetes manager. It must return JSON containing a "version" field.
	// When ConnectionToken is configured, the poller sends it as a Bearer token.
	UpgradeVersionURL string                                  `json:"upgrade_version_url" mapstructure:"upgrade_version_url"`
	DeploymentName    string                                  `json:"deployment_name" mapstructure:"deployment_name"`
	ImageRepository   string                                  `json:"image_repository" mapstructure:"image_repository"`
	CurrentVersion    string                                  `json:"current_version" mapstructure:"current_version"`
	Allocation        SessionManagerAllocationConfig          `json:"allocation" mapstructure:"allocation"`
	Builtin           SessionManagerBuiltinRegistrationConfig `json:"builtin" mapstructure:"builtin"`
}

type SessionManagerAllocationConfig struct {
	LeaseDuration string `json:"lease_duration" mapstructure:"lease_duration"`
	RenewDeadline string `json:"renew_deadline" mapstructure:"renew_deadline"`
	RetryPeriod   string `json:"retry_period" mapstructure:"retry_period"`
	LeaseName     string `json:"lease_name" mapstructure:"lease_name"`
}

type SessionManagerBuiltinBindingConfig struct {
	SubjectType string `json:"subject_type" mapstructure:"subject_type"`
	Role        string `json:"role" mapstructure:"role"`
}

type SessionManagerBuiltinPoolConfig struct {
	Name         string                             `json:"name" mapstructure:"name"`
	Labels       map[string]string                  `json:"labels" mapstructure:"labels"`
	LabelsJSON   string                             `json:"labels_json" mapstructure:"labels_json"`
	ExplicitOnly bool                               `json:"explicit_only" mapstructure:"explicit_only"`
	Priority     int                                `json:"priority" mapstructure:"priority"`
	Binding      SessionManagerBuiltinBindingConfig `json:"binding" mapstructure:"binding"`
}

type SessionManagerBuiltinRegistrationConfig struct {
	Enabled         bool                            `json:"enabled" mapstructure:"enabled"`
	ManagerID       string                          `json:"manager_id" mapstructure:"manager_id"`
	Name            string                          `json:"name" mapstructure:"name"`
	ConnectionToken string                          `json:"connection_token" mapstructure:"connection_token"`
	Pool            SessionManagerBuiltinPoolConfig `json:"pool" mapstructure:"pool"`
}
