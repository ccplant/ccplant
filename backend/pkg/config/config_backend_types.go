package config

import (
	"strings"
)

// WorkerConfig contains the only control-plane dependency of the worker.
// Worker persistence and leader election use KVStore and Redis respectively.
type WorkerConfig struct {
	ControlAPIURL            string `json:"control_api_url" mapstructure:"control_api_url"`
	ControlAPIToken          string `json:"control_api_token" mapstructure:"control_api_token"`
	ExecutionTokenSigningKey string `json:"execution_token_signing_key" mapstructure:"execution_token_signing_key"`
	SessionAPIURL            string `json:"session_api_url" mapstructure:"session_api_url"`
}

// RedisConfig holds configuration for the optional Redis backend used for
// cross-pod session status synchronisation.
type RedisConfig struct {
	// Addr is the Redis server address in "host:port" format (e.g. "redis:6379").
	// When empty, a no-op in-memory fallback is used and no real connection is made.
	Addr string `json:"addr" mapstructure:"addr"`
	// Password is the Redis AUTH password (optional).
	Password string `json:"password" mapstructure:"password"`
	// DB is the Redis database index to use (default: 0).
	DB int `json:"db" mapstructure:"db"`
	// TLSEnabled enables TLS for the Redis connection.
	TLSEnabled bool `json:"tls_enabled" mapstructure:"tls_enabled"`
	// DialTimeout is the timeout for establishing a connection (e.g. "5s").
	DialTimeout string `json:"dial_timeout" mapstructure:"dial_timeout"`
	// ReadTimeout is the timeout for socket reads (e.g. "3s").
	ReadTimeout string `json:"read_timeout" mapstructure:"read_timeout"`
	// WriteTimeout is the timeout for socket writes (e.g. "3s").
	WriteTimeout string `json:"write_timeout" mapstructure:"write_timeout"`
	// SessionControlPoolSize limits concurrent short session-control commands.
	SessionControlPoolSize int `json:"session_control_pool_size" mapstructure:"session_control_pool_size"`
	// SessionControlBlockingPoolSize limits concurrent session-control long polls.
	SessionControlBlockingPoolSize int `json:"session_control_blocking_pool_size" mapstructure:"session_control_blocking_pool_size"`
}

// KVStoreConfig configures persistence for application data represented as
// Kubernetes Secrets and ConfigMaps by the repository layer.
type KVStoreBackendConfig struct {
	Backend     string                  `json:"backend" mapstructure:"backend"`
	DatabaseURL string                  `json:"database_url" mapstructure:"database_url"`
	AuthToken   string                  `json:"auth_token" mapstructure:"auth_token"`
	Encryption  KVStoreEncryptionConfig `json:"encryption" mapstructure:"encryption"`
}

type KVStoreReplicationConfig struct {
	Mode string `json:"mode" mapstructure:"mode"`
}

type KVStoreEncryptionConfig struct {
	Provider              string            `json:"provider" mapstructure:"provider"`
	ActiveKeyID           string            `json:"active_key_id" mapstructure:"active_key_id"`
	KMSKeyID              string            `json:"kms_key_id" mapstructure:"kms_key_id"`
	KMSRegion             string            `json:"kms_region" mapstructure:"kms_region"`
	Keys                  map[string]string `json:"keys" mapstructure:"keys"`
	BranchCacheTTLSeconds int               `json:"branch_cache_ttl_seconds" mapstructure:"branch_cache_ttl_seconds"`
	BranchCacheMaxEntries int               `json:"branch_cache_max_entries" mapstructure:"branch_cache_max_entries"`
}

// EffectiveKeys returns the configured keyring, expanding the single-key KMS
// form used by Kubernetes Secret references when a full key map is absent.
func (c KVStoreEncryptionConfig) EffectiveKeys() map[string]string {
	if len(c.Keys) > 0 {
		return c.Keys
	}
	if c.ActiveKeyID == "" || strings.TrimSpace(c.KMSKeyID) == "" {
		return nil
	}
	return map[string]string{c.ActiveKeyID: c.KMSKeyID}
}

type KVStoreConfig struct {
	// Legacy single-backend fields. They remain supported as primary-only configuration.
	Namespace   string                   `json:"namespace" mapstructure:"namespace"`
	Backend     string                   `json:"backend" mapstructure:"backend"`
	DatabaseURL string                   `json:"database_url" mapstructure:"database_url"`
	AuthToken   string                   `json:"auth_token" mapstructure:"auth_token"`
	Primary     *KVStoreBackendConfig    `json:"primary" mapstructure:"primary"`
	Secondary   *KVStoreBackendConfig    `json:"secondary" mapstructure:"secondary"`
	Replication KVStoreReplicationConfig `json:"replication" mapstructure:"replication"`
	Encryption  KVStoreEncryptionConfig  `json:"encryption" mapstructure:"encryption"`
}

// UsageConfig configures the dedicated libSQL database used for usage events.
// It is intentionally independent from KVStoreConfig.
type UsageConfig struct {
	Enabled     bool   `json:"enabled" mapstructure:"enabled"`
	DatabaseURL string `json:"database_url" mapstructure:"database_url"`
	AuthToken   string `json:"auth_token" mapstructure:"auth_token"`
}

// SessionCountConfig configures append-only session status usage events.
type SessionCountConfig struct {
	Enabled     bool   `json:"enabled" mapstructure:"enabled"`
	Backend     string `json:"backend" mapstructure:"backend"`
	DatabaseURL string `json:"database_url" mapstructure:"database_url"`
	AuthToken   string `json:"auth_token" mapstructure:"auth_token"`
}
