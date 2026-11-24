package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	defer func() { assert.NoError(t, os.Remove(tmpfile.Name())) }()

	if _, err := tmpfile.WriteString(yamlConfig); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	assert.NoError(t, tmpfile.Close())

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
	t.Setenv("AGENTAPI_AUTH_ENABLED", "true")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "env_client_id")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "env_client_secret")

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

	t.Setenv("AGENTAPI_STOCK_INVENTORY_WORKER_POOLS", `[
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

	t.Setenv("AGENTAPI_SCIA_ENABLED", "true")
	t.Setenv("AGENTAPI_SCIA_PUBLIC_BASE_URL", "https://agentapi.example.com")
	t.Setenv("AGENTAPI_SCIA_CREDENTIAL", "default.google")
	t.Setenv("AGENTAPI_SCIA_USER_NAMESPACE", "default")
	t.Setenv("AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED", "true")
	t.Setenv("AGENTAPI_SCIA_SESSION_SIDECAR_PORT", "18082")
	t.Setenv("AGENTAPI_SCIA_GOOGLE_HOSTS", "www.googleapis.com,content.googleapis.com")
	t.Setenv("AGENTAPI_SCIA_GOOGLE_PATHS", "/calendar/v3/*,/drive/v3/*")
	t.Setenv("AGENTAPI_SCIA_TODOIST_CREDENTIAL", "default.todoist")
	t.Setenv("AGENTAPI_SCIA_TODOIST_HOSTS", "api.todoist.com")
	t.Setenv("AGENTAPI_SCIA_TODOIST_PATHS", "/api/v1/*")

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
	t.Setenv("AGENTAPI_AUTH_ADMIN_KEY", "ap_admin_env_key")

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
	t.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	t.Setenv("AGENTAPI_AUTH_GITHUB_BASE_URL", "https://github.company.com/api/v3")
	t.Setenv("AGENTAPI_AUTH_GITHUB_TOKEN_HEADER", "X-GitHub-Token")
	t.Setenv("AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE", "developer")

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
	t.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "oauth_client_123")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "oauth_secret_456")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE", "read:user read:org repo")
	t.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL", "https://github.company.com")

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
	t.Setenv("AGENTAPI_AUTH_ADMIN_KEY", "ap_admin_env_key")
	t.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")

	// Create config with existing auth structures
	configJSON := `{"auth":{"enabled":false,"admin_key":"ap_file_key","github":{"enabled":false,"base_url":"https://existing.github.com"}}}`

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
		t.Setenv(key, value)
	}

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
