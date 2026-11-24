package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// clearAGENTAPIEnvVars clears AGENTAPI_ variables and restores their exact
// original presence and value when the test finishes.
func clearAGENTAPIEnvVars(t *testing.T) {
	t.Helper()
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "AGENTAPI_") {
			parts := strings.SplitN(env, "=", 2)
			key := parts[0]
			t.Setenv(key, "")
			assert.NoError(t, os.Unsetenv(key))
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	// Verify default auth config
	if config.Auth.AdminKey != "" {
		t.Error("Auth.AdminKey should be empty by default")
	}
	if config.Auth.BootstrapAdmin == nil {
		t.Error("Auth.BootstrapAdmin should be initialized by default")
	}
}

func TestLoadConfigDefaultsEmptyKubernetesSessionBasePort(t *testing.T) {
	clearAGENTAPIEnvVars(t)
	t.Setenv("AGENTAPI_K8S_SESSION_BASE_PORT", "")

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, 9000, loadedConfig.KubernetesSession.BasePort)
}

func TestLoadConfigSessionControlPoolSizes(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, 50, loadedConfig.Redis.SessionControlPoolSize)
	assert.Equal(t, 400, loadedConfig.Redis.SessionControlBlockingPoolSize)

	t.Setenv("AGENTAPI_REDIS_SESSION_CONTROL_POOL_SIZE", "12")
	t.Setenv("AGENTAPI_REDIS_SESSION_CONTROL_BLOCKING_POOL_SIZE", "120")
	loadedConfig, err = LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, 12, loadedConfig.Redis.SessionControlPoolSize)
	assert.Equal(t, 120, loadedConfig.Redis.SessionControlBlockingPoolSize)
}

func TestLoadConfigSessionManagerLeaseNameCompatibility(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "agentapi-session-allocator", loadedConfig.SessionManager.Allocation.LeaseName)

	t.Setenv("AGENTAPI_SESSION_MANAGER_ALLOCATION_LEASE_NAME", "release-a-session-manager")
	loadedConfig, err = LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "release-a-session-manager", loadedConfig.SessionManager.Allocation.LeaseName)
}

func TestLoadConfigSessionPersistenceS3DedicatedCredentials(t *testing.T) {
	clearAGENTAPIEnvVars(t)
	t.Setenv("AGENTAPI_SESSION_PERSISTENCE_S3_BUCKET", "storage-bucket")
	t.Setenv("AGENTAPI_SESSION_PERSISTENCE_S3_ACCESS_KEY_ID", "storage-access")
	t.Setenv("AGENTAPI_SESSION_PERSISTENCE_S3_SECRET_ACCESS_KEY", "storage-secret")

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	if assert.NotNil(t, loadedConfig.SessionPersistence.S3) {
		assert.Equal(t, "storage-access", loadedConfig.SessionPersistence.S3.AccessKeyID)
		assert.Equal(t, "storage-secret", loadedConfig.SessionPersistence.S3.SecretAccessKey)
	}
}

func TestLoadConfigSessionTTLDefaults(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Empty(t, loadedConfig.ScheduleSessionTTL)
	assert.Empty(t, loadedConfig.WebhookSessionTTL)
	assert.Empty(t, loadedConfig.InteractiveSessionTTL)

	valid := map[string]string{
		"AGENTAPI_SCHEDULE_SESSION_TTL":    "24h",
		"AGENTAPI_WEBHOOK_SESSION_TTL":     "48h",
		"AGENTAPI_INTERACTIVE_SESSION_TTL": "168h",
	}
	for name, value := range valid {
		t.Setenv(name, value)
	}
	loadedConfig, err = LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "24h", loadedConfig.ScheduleSessionTTL)
	assert.Equal(t, "48h", loadedConfig.WebhookSessionTTL)
	assert.Equal(t, "168h", loadedConfig.InteractiveSessionTTL)

	for name := range valid {
		for _, invalid := range []string{"1d", "0s", "-1h"} {
			for other, value := range valid {
				t.Setenv(other, value)
			}
			t.Setenv(name, invalid)
			_, err = LoadConfig("")
			assert.Error(t, err, name+"="+invalid)
		}
	}
}

func TestLoadConfigKubernetesSessionIsolationFromEnv(t *testing.T) {
	clearAGENTAPIEnvVars(t)
	t.Setenv("AGENTAPI_K8S_SESSION_DISABLE_SERVICE_LINKS", "true")
	t.Setenv("AGENTAPI_K8S_SESSION_DISABLE_SERVICE_ACCOUNT_TOKEN", "true")

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.True(t, loadedConfig.KubernetesSession.DisableServiceLinks)
	assert.True(t, loadedConfig.KubernetesSession.DisableServiceAccountToken)
}

func TestLoadK8sSessionConfigFromYAMLUsesStringKeyedAffinityMaps(t *testing.T) {
	configFile := t.TempDir() + "/k8s-session-config.yaml"
	err := os.WriteFile(configFile, []byte(`
kubernetes_session:
  node_selector:
    storage: hci50k-a05
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: storage
                operator: In
                values: [hci50k-a05]
`), 0o600)
	assert.NoError(t, err)

	loaded := DefaultConfig()
	assert.NoError(t, loadK8sSessionConfigFromFile(loaded, configFile))
	assert.Equal(t, "hci50k-a05", loaded.KubernetesSession.NodeSelector["storage"])
	assert.NotPanics(t, func() {
		_, err = json.Marshal(loaded)
	})
	assert.NoError(t, err)

	nodeAffinity, ok := loaded.KubernetesSession.Affinity["nodeAffinity"].(map[string]interface{})
	assert.True(t, ok, "nested affinity map must have string keys")
	assert.Contains(t, nodeAffinity, "requiredDuringSchedulingIgnoredDuringExecution")
}

func TestBinaryPathConfig(t *testing.T) {
	t.Setenv("CCPLANT_BINARY_PATH", "/opt/ccplant/bin/ccplant")
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if got, want := loadedConfig.BinaryPath, "/opt/ccplant/bin/ccplant"; got != want {
		t.Fatalf("BinaryPath = %q, want %q", got, want)
	}
}

func TestLoadConfig(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Create a temporary config file
	tempConfig := &Config{
		Auth: AuthConfig{},
	}

	configData, err := json.Marshal(tempConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { assert.NoError(t, os.Remove(tmpfile.Name())) }()

	if _, err := tmpfile.Write(configData); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	assert.NoError(t, tmpfile.Close())

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify auth config defaults
	if loadedConfig.Auth.AdminKey != "" {
		t.Error("Auth.AdminKey should be empty by default")
	}

	// Verify GitHub auth config is properly initialized with defaults
	if loadedConfig.Auth.GitHub == nil {
		t.Error("Auth.GitHub should not be nil")
	} else {
		if loadedConfig.Auth.GitHub.BaseURL != "https://api.github.com" {
			t.Errorf("Auth.GitHub.BaseURL should be 'https://api.github.com', got '%s'", loadedConfig.Auth.GitHub.BaseURL)
		}
		if loadedConfig.Auth.GitHub.TokenHeader != "Authorization" {
			t.Errorf("Auth.GitHub.TokenHeader should be 'Authorization', got '%s'", loadedConfig.Auth.GitHub.TokenHeader)
		}
		if loadedConfig.Auth.GitHub.OAuth == nil {
			t.Error("Auth.GitHub.OAuth should not be nil")
		} else if loadedConfig.Auth.GitHub.OAuth.Scope != "read:user read:org project" {
			t.Errorf("Auth.GitHub.OAuth.Scope should be 'read:user read:org project', got '%s'", loadedConfig.Auth.GitHub.OAuth.Scope)
		}
	}
}

func TestLoadConfigKVStoreFromEnvironment(t *testing.T) {
	clearAGENTAPIEnvVars(t)
	t.Setenv("AGENTAPI_KV_STORE_BACKEND", "libsql")
	t.Setenv("AGENTAPI_KV_STORE_DATABASE_URL", "https://example.turso.io")
	t.Setenv("AGENTAPI_KV_STORE_AUTH_TOKEN", "secret-token")

	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	assert.Equal(t, "libsql", loadedConfig.KVStore.Backend)
	assert.Equal(t, "https://example.turso.io", loadedConfig.KVStore.DatabaseURL)
	assert.Equal(t, "secret-token", loadedConfig.KVStore.AuthToken)
}

func TestLoadConfigNonexistentFile(t *testing.T) {
	_, err := LoadConfig("nonexistent-file.json")
	if err == nil {
		t.Error("LoadConfig should return error for nonexistent file")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	// Create a temporary file with invalid JSON
	tmpfile, err := os.CreateTemp("", "invalid*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { assert.NoError(t, os.Remove(tmpfile.Name())) }()

	invalidJSON := `{"invalid": json}`
	if _, err := tmpfile.WriteString(invalidJSON); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}
	assert.NoError(t, tmpfile.Close())

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("LoadConfig should return error for invalid JSON")
	}
}
func TestExpandEnvVars(t *testing.T) {
	// Set up test environment variables
	t.Setenv("TEST_VAR", "test_value")
	t.Setenv("CLIENT_ID", "my_client_id")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple variable expansion",
			input:    "${TEST_VAR}",
			expected: "test_value",
		},
		{
			name:     "Variable in string",
			input:    "prefix_${TEST_VAR}_suffix",
			expected: "prefix_test_value_suffix",
		},
		{
			name:     "Multiple variables",
			input:    "${TEST_VAR}_${CLIENT_ID}",
			expected: "test_value_my_client_id",
		},
		{
			name:     "Non-existent variable",
			input:    "${NON_EXISTENT_VAR}",
			expected: "${NON_EXISTENT_VAR}",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "No variables",
			input:    "plain_string",
			expected: "plain_string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadConfigWithEnvVarExpansion(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Set up test environment variables
	t.Setenv("TEST_CLIENT_ID", "github_client_123")
	t.Setenv("TEST_CLIENT_SECRET", "github_secret_456")

	// Create config with environment variable references
	configJSON := `{
		"auth": {
			"enabled": true,
			"github": {
				"enabled": true,
				"oauth": {
					"client_id": "${TEST_CLIENT_ID}",
					"client_secret": "${TEST_CLIENT_SECRET}",
					"scope": "read:user read:org"
				}
			}
		}
	}`

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { assert.NoError(t, os.Remove(tmpfile.Name())) }()

	if _, err := tmpfile.WriteString(configJSON); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	assert.NoError(t, tmpfile.Close())

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables were expanded
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "github_client_123" {
		t.Errorf("Expected ClientID to be 'github_client_123', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientSecret != "github_secret_456" {
		t.Errorf("Expected ClientSecret to be 'github_secret_456', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	}
}
