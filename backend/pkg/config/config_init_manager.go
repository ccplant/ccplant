package config

import (
	"encoding/json"
	"log"
	"os"
	"strconv"

	"github.com/spf13/viper"
)

func initializeBuiltinManagerEnv(config *Config) {
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_ENABLED"); ok {
		config.SessionManager.Builtin.Enabled, _ = strconv.ParseBool(value)
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_MANAGER_ID"); ok {
		config.SessionManager.Builtin.ManagerID = value
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_NAME"); ok {
		config.SessionManager.Builtin.Name = value
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_CONNECTION_TOKEN"); ok {
		config.SessionManager.Builtin.ConnectionToken = value
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL"); ok {
		config.SessionManager.Builtin.Pool.Name = value
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_EXPLICIT_ONLY"); ok {
		config.SessionManager.Builtin.Pool.ExplicitOnly, _ = strconv.ParseBool(value)
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_PRIORITY"); ok {
		config.SessionManager.Builtin.Pool.Priority, _ = strconv.Atoi(value)
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_BINDING_SUBJECT_TYPE"); ok {
		config.SessionManager.Builtin.Pool.Binding.SubjectType = value
	}
	if value, ok := os.LookupEnv("AGENTAPI_SESSION_MANAGER_BUILTIN_BINDING_ROLE"); ok {
		config.SessionManager.Builtin.Pool.Binding.Role = value
	}
	if labelsJSON := os.Getenv("AGENTAPI_SESSION_MANAGER_BUILTIN_POOL_LABELS"); labelsJSON != "" {
		var labels map[string]string
		if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
			log.Printf("[CONFIG] Warning: invalid built-in session manager pool labels: %v", err)
		} else {
			config.SessionManager.Builtin.Pool.Labels = labels
		}
	}
}

func initializePersistenceEnv(config *Config, v *viper.Viper) {
	config.SessionPersistence.Backend = v.GetString("session_persistence.backend")
	config.SessionPersistence.Path = v.GetString("session_persistence.path")
	config.SessionPersistence.SuspendAfter = v.GetString("session_persistence.suspend_after")
	if bucket := v.GetString("session_persistence.s3.bucket"); bucket != "" {
		config.SessionPersistence.S3 = &S3StorageConfig{
			Bucket: bucket, Region: v.GetString("session_persistence.s3.region"),
			Prefix: v.GetString("session_persistence.s3.prefix"), Endpoint: v.GetString("session_persistence.s3.endpoint"),
			AccessKeyID: v.GetString("session_persistence.s3.access_key_id"), SecretAccessKey: v.GetString("session_persistence.s3.secret_access_key"),
		}
	}
}
