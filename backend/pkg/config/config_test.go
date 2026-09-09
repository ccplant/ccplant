package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// clearAGENTAPIEnvVars clears all AGENTAPI_ environment variables and returns a cleanup function
func clearAGENTAPIEnvVars(t *testing.T) {
	t.Helper()
	savedEnvVars := make(map[string]string)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "AGENTAPI_") {
			parts := strings.SplitN(env, "=", 2)
			key := parts[0]
			savedEnvVars[key] = os.Getenv(key)
			_ = os.Unsetenv(key)
		}
	}
	t.Cleanup(func() {
		// Restore environment variables
		for key, value := range savedEnvVars {
			if value != "" {
				_ = os.Setenv(key, value)
			}
		}
	})
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
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write(configData); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

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
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	invalidJSON := `{"invalid": json}`
	if _, err := tmpfile.WriteString(invalidJSON); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}
	_ = tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("LoadConfig should return error for invalid JSON")
	}
}
func TestExpandEnvVars(t *testing.T) {
	// Set up test environment variables
	_ = os.Setenv("TEST_VAR", "test_value")
	_ = os.Setenv("CLIENT_ID", "my_client_id")
	defer func() {
		_ = os.Unsetenv("TEST_VAR")
		_ = os.Unsetenv("CLIENT_ID")
	}()

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
	_ = os.Setenv("TEST_CLIENT_ID", "github_client_123")
	_ = os.Setenv("TEST_CLIENT_SECRET", "github_secret_456")
	defer func() {
		_ = os.Unsetenv("TEST_CLIENT_ID")
		_ = os.Unsetenv("TEST_CLIENT_SECRET")
	}()

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
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(configJSON); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

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

func TestLoadConfigWithYAML(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Create YAML config
	yamlConfig := `
auth:
  enabled: true
  github:
    enabled: true
    oauth:
      client_id: "yaml_client_id"
      client_secret: "yaml_client_secret"
      scope: "read:user read:org"
`

	// Write to temporary YAML file
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(yamlConfig); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify YAML was loaded correctly
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "yaml_client_id" {
		t.Errorf("Expected ClientID to be 'yaml_client_id', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}
}

func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	// Set up test environment variables (viper format)
	_ = os.Setenv("AGENTAPI_AUTH_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "env_client_id")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "env_client_secret")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET")
	}()

	// Load config without specifying a file (should use env vars and defaults)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables were loaded
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	// GitHub auth is considered enabled if OAuth config is present
	// (Auth.GitHub.Enabled was removed in favor of checking individual auth methods)

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "env_client_id" {
		t.Errorf("Expected ClientID to be 'env_client_id', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientSecret != "env_client_secret" {
		t.Errorf("Expected ClientSecret to be 'env_client_secret', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	}
}

func TestLoadConfigWithKubernetesGitHubSecretEnvironmentVariables(t *testing.T) {
	clearAGENTAPIEnvVars(t)
	t.Setenv("AGENTAPI_K8S_SESSION_GITHUB_SECRET_NAME", "github-session")
	t.Setenv("AGENTAPI_K8S_SESSION_GITHUB_CONFIG_SECRET_NAME", "github-config")

	loadedConfig, err := LoadConfig("")
	assert.NoError(t, err)
	assert.Equal(t, "github-session", loadedConfig.KubernetesSession.GitHubSecretName)
	assert.Equal(t, "github-config", loadedConfig.KubernetesSession.GitHubConfigSecretName)
}

func TestLoadConfigWithStockInventoryPoolsEnv(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	_ = os.Setenv("AGENTAPI_STOCK_INVENTORY_WORKER_POOLS", `[
		{"targetCount":1,"dockerEnabled":false},
		{"targetCount":2,"dockerEnabled":false},
		{"target_count":3,"docker_enabled":true}
	]`)

	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	assert.Equal(t, []StockInventoryPoolConfig{
		{TargetCount: 1, DockerEnabled: false},
		{TargetCount: 2, DockerEnabled: false},
		{TargetCount: 3, DockerEnabled: true},
	}, loadedConfig.StockInventoryWorker.Pools)
}

func TestLoadConfigWithSciaEnvironmentVariables(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	_ = os.Setenv("AGENTAPI_SCIA_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_SCIA_PUBLIC_BASE_URL", "https://agentapi.example.com")
	_ = os.Setenv("AGENTAPI_SCIA_CREDENTIAL", "default.google")
	_ = os.Setenv("AGENTAPI_SCIA_USER_NAMESPACE", "default")
	_ = os.Setenv("AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_SCIA_SESSION_SIDECAR_PORT", "18082")
	_ = os.Setenv("AGENTAPI_SCIA_GOOGLE_HOSTS", "www.googleapis.com,content.googleapis.com")
	_ = os.Setenv("AGENTAPI_SCIA_GOOGLE_PATHS", "/calendar/v3/*,/drive/v3/*")
	_ = os.Setenv("AGENTAPI_SCIA_TODOIST_CREDENTIAL", "default.todoist")
	_ = os.Setenv("AGENTAPI_SCIA_TODOIST_HOSTS", "api.todoist.com")
	_ = os.Setenv("AGENTAPI_SCIA_TODOIST_PATHS", "/api/v1/*")

	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	assert.True(t, loadedConfig.Scia.Enabled)
	assert.Equal(t, "https://agentapi.example.com", loadedConfig.Scia.PublicBaseURL)
	assert.Equal(t, "default.google", loadedConfig.Scia.Credential)
	assert.Equal(t, "default", loadedConfig.Scia.UserNamespace)
	assert.True(t, loadedConfig.Scia.SessionSidecarEnabled)
	assert.Equal(t, 18082, loadedConfig.Scia.SessionSidecarPort)
	assert.Equal(t, []string{"www.googleapis.com", "content.googleapis.com"}, loadedConfig.Scia.GoogleHosts)
	assert.Equal(t, []string{"/calendar/v3/*", "/drive/v3/*"}, loadedConfig.Scia.GooglePaths)
	assert.Equal(t, "default.todoist", loadedConfig.Scia.TodoistCredential)
	assert.Equal(t, []string{"api.todoist.com"}, loadedConfig.Scia.TodoistHosts)
	assert.Equal(t, []string{"/api/v1/*"}, loadedConfig.Scia.TodoistPaths)
}

func TestLoadConfigNetworkFilterResourceDefaults(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	assert.Equal(t, "250m", loadedConfig.KubernetesSession.NetworkFilterCPURequest)
	assert.Equal(t, "1000m", loadedConfig.KubernetesSession.NetworkFilterCPULimit)
	assert.Equal(t, "256Mi", loadedConfig.KubernetesSession.NetworkFilterMemoryRequest)
	assert.Equal(t, "512Mi", loadedConfig.KubernetesSession.NetworkFilterMemoryLimit)
	assert.Equal(t, "50m", loadedConfig.KubernetesSession.NetworkFilterInitCPURequest)
	assert.Equal(t, "100m", loadedConfig.KubernetesSession.NetworkFilterInitCPULimit)
	assert.Equal(t, "32Mi", loadedConfig.KubernetesSession.NetworkFilterInitMemoryRequest)
	assert.Equal(t, "64Mi", loadedConfig.KubernetesSession.NetworkFilterInitMemoryLimit)
}

func TestInitializeConfigStructsFromEnv_AdminKey(t *testing.T) {
	// Set up admin key environment variable
	_ = os.Setenv("AGENTAPI_AUTH_ADMIN_KEY", "ap_admin_env_key")

	defer func() { _ = os.Unsetenv("AGENTAPI_AUTH_ADMIN_KEY") }()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	assert.Equal(t, "ap_admin_env_key", loadedConfig.Auth.AdminKey)
}

func TestInitializeConfigStructsFromEnv_GitHubAuth(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Set up test environment variables for GitHub auth
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_BASE_URL", "https://github.company.com/api/v3")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_TOKEN_HEADER", "X-GitHub-Token")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE", "developer")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_BASE_URL")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_TOKEN_HEADER")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify GitHub auth config was initialized from environment variables
	if loadedConfig.Auth.GitHub == nil {
		t.Fatal("Auth.GitHub should not be nil when environment variables are set")
	}

	assert.True(t, loadedConfig.Auth.GitHub.Enabled)
	assert.Equal(t, "https://github.company.com/api/v3", loadedConfig.Auth.GitHub.BaseURL)
	assert.Equal(t, "X-GitHub-Token", loadedConfig.Auth.GitHub.TokenHeader)
	assert.Equal(t, "developer", loadedConfig.Auth.GitHub.UserMapping.DefaultRole)
}

func TestInitializeConfigStructsFromEnv_GitHubOAuth(t *testing.T) {
	// First set up GitHub auth to exist
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "oauth_client_123")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "oauth_secret_456")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE", "read:user read:org repo")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL", "https://github.company.com")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify GitHub OAuth config was initialized from environment variables
	if loadedConfig.Auth.GitHub == nil {
		t.Fatal("Auth.GitHub should not be nil")
	}
	if loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("Auth.GitHub.OAuth should not be nil when environment variables are set")
	}

	assert.Equal(t, "oauth_client_123", loadedConfig.Auth.GitHub.OAuth.ClientID)
	assert.Equal(t, "oauth_secret_456", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	assert.Equal(t, "read:user read:org repo", loadedConfig.Auth.GitHub.OAuth.Scope)
	assert.Equal(t, "https://github.company.com", loadedConfig.Auth.GitHub.OAuth.BaseURL)
}

func TestInitializeConfigStructsFromEnv_NoInitializationWhenConfigExists(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Set up environment variables
	_ = os.Setenv("AGENTAPI_AUTH_ADMIN_KEY", "ap_admin_env_key")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_ADMIN_KEY")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
	}()

	// Create config with existing auth structures
	configJSON := `{"auth":{"enabled":false,"admin_key":"ap_file_key","github":{"enabled":false,"base_url":"https://existing.github.com"}}}`

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(configJSON); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables take precedence for scalar auth values,
	// while struct fields already set from the file keep their non-env values.
	assert.Equal(t, "ap_admin_env_key", loadedConfig.Auth.AdminKey)                  // Environment variable takes precedence
	assert.True(t, loadedConfig.Auth.GitHub.Enabled)                                 // Environment variable takes precedence
	assert.Equal(t, "https://existing.github.com", loadedConfig.Auth.GitHub.BaseURL) // Should remain as configured
}

func TestInitializeConfigStructsFromEnv_AllSettingsFromEnvironment(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Set up comprehensive environment variables
	envVars := map[string]string{
		"AGENTAPI_AUTH_ENABLED":                          "true",
		"AGENTAPI_AUTH_ADMIN_KEY":                        "ap_admin_full_test",
		"AGENTAPI_AUTH_GITHUB_ENABLED":                   "true",
		"AGENTAPI_AUTH_GITHUB_BASE_URL":                  "https://full.test.github.com/api/v3",
		"AGENTAPI_AUTH_GITHUB_TOKEN_HEADER":              "X-Full-GitHub-Token",
		"AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE": "full-tester",
		"AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID":           "full_client_123",
		"AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET":       "full_secret_456",
		"AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE":               "read:user read:org admin:repo",
		"AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL":            "https://full.test.github.com",
	}

	// Set all environment variables
	for key, value := range envVars {
		_ = os.Setenv(key, value)
	}

	// Clean up environment variables
	defer func() {
		for key := range envVars {
			_ = os.Unsetenv(key)
		}
	}()

	// Load config without file (should initialize everything from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify all settings were loaded from environment variables
	// Admin key verification
	assert.Equal(t, "ap_admin_full_test", loadedConfig.Auth.AdminKey)

	// GitHub auth verification
	if assert.NotNil(t, loadedConfig.Auth.GitHub) {
		assert.True(t, loadedConfig.Auth.GitHub.Enabled)
		assert.Equal(t, "https://full.test.github.com/api/v3", loadedConfig.Auth.GitHub.BaseURL)
		assert.Equal(t, "X-Full-GitHub-Token", loadedConfig.Auth.GitHub.TokenHeader)
		assert.Equal(t, "full-tester", loadedConfig.Auth.GitHub.UserMapping.DefaultRole)

		// GitHub OAuth verification
		if assert.NotNil(t, loadedConfig.Auth.GitHub.OAuth) {
			assert.Equal(t, "full_client_123", loadedConfig.Auth.GitHub.OAuth.ClientID)
			assert.Equal(t, "full_secret_456", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
			assert.Equal(t, "read:user read:org admin:repo", loadedConfig.Auth.GitHub.OAuth.Scope)
			assert.Equal(t, "https://full.test.github.com", loadedConfig.Auth.GitHub.OAuth.BaseURL)
		}
	}

}
