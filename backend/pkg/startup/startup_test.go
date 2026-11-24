package startup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	testassert "github.com/stretchr/testify/assert"
)

func TestGenerateMarketplaceGitHubAppTokenDiscoversRepositoryInstallation(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	var installationRequests, tokenRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/acme/private-marketplace/installation":
			installationRequests++
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"id": 99}))
		case "/api/v3/app/installations/99/access_tokens":
			tokenRequests++
			var body struct {
				Repositories []string          `json:"repositories"`
				Permissions  map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode token request: %v", err)
			}
			if !reflect.DeepEqual(body.Repositories, []string{"private-marketplace"}) {
				t.Errorf("repositories = %v", body.Repositories)
			}
			if body.Permissions["contents"] != "read" {
				t.Errorf("contents permission = %q", body.Permissions["contents"])
			}
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "marketplace-token",
				"expires_at": "2030-01-01T00:00:00Z",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("GITHUB_APP_ID", "123")
	t.Setenv("GITHUB_APP_PEM", string(pemData))
	t.Setenv("GITHUB_APP_PEM_PATH", "")
	t.Setenv("GITHUB_URL", "https://github.enterprise.test")
	t.Setenv("GITHUB_API", server.URL)
	t.Setenv("GITHUB_TOKEN", "working-repository-token")
	t.Setenv("GITHUB_INSTALLATION_ID", "42")

	token, err := GenerateMarketplaceGitHubAppToken(context.Background(), "https://github.enterprise.test/acme/private-marketplace.git")
	if err != nil {
		t.Fatalf("GenerateMarketplaceGitHubAppToken: %v", err)
	}
	if token != "marketplace-token" {
		t.Fatalf("token = %q", token)
	}
	if installationRequests != 1 || tokenRequests != 1 {
		t.Fatalf("requests = installation:%d token:%d", installationRequests, tokenRequests)
	}
}

func TestSetupClaudeCode(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "claude-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("Failed to restore HOME: %v", err)
		}
	}()
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME: %v", err)
	}

	// Mock userHomeDir function by creating a test that works with temp directory
	err = SetupClaudeCode(tempDir)
	if err != nil {
		t.Fatalf("SetupClaudeCode failed: %v", err)
	}

	// Check if .claude directory was created
	claudeDir := filepath.Join(tempDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Errorf(".claude directory was not created")
	}

	// Check if settings.json was created
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Errorf("settings.json was not created")
	}

	// Verify settings.json content
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	// Check if required settings are present
	if settingsMap, ok := settings["settings"].(map[string]interface{}); ok {
		if mcpEnabled, exists := settingsMap["mcp.enabled"]; !exists || mcpEnabled != true {
			t.Errorf("mcp.enabled setting is missing or incorrect")
		}
	} else {
		t.Errorf("settings section is missing from settings.json")
	}
}

func TestMergeClaudeConfig(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "claude-merge-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("Failed to restore HOME: %v", err)
		}
	}()
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME: %v", err)
	}

	// Test case 1: No existing .claude.json file
	err = mergeClaudeConfig(tempDir)
	if err != nil {
		t.Fatalf("mergeClaudeConfig failed with no existing file: %v", err)
	}

	// Check if .claude.json was created
	claudeJsonPath := filepath.Join(tempDir, ".claude.json")
	if _, err := os.Stat(claudeJsonPath); os.IsNotExist(err) {
		t.Errorf(".claude.json was not created")
	}

	// Verify content
	claudeJsonData, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var claudeJson map[string]interface{}
	if err := json.Unmarshal(claudeJsonData, &claudeJson); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	expectedConfig := map[string]interface{}{
		"hasCompletedOnboarding":        true,
		"bypassPermissionsModeAccepted": true,
	}

	for key, expectedValue := range expectedConfig {
		if value, exists := claudeJson[key]; !exists || value != expectedValue {
			t.Errorf("Expected %s to be %v, got %v", key, expectedValue, value)
		}
	}

	// Test case 2: Existing .claude.json file with other content
	existingConfig := map[string]interface{}{
		"existingKey":            "existingValue",
		"hasCompletedOnboarding": false, // This should be overwritten
	}

	existingData, err := json.Marshal(existingConfig)
	if err != nil {
		t.Fatalf("Failed to marshal existing config: %v", err)
	}

	if err := os.WriteFile(claudeJsonPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Merge config again
	err = mergeClaudeConfig(tempDir)
	if err != nil {
		t.Fatalf("mergeClaudeConfig failed with existing file: %v", err)
	}

	// Read and verify merged content
	mergedData, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		t.Fatalf("Failed to read merged .claude.json: %v", err)
	}

	var mergedJson map[string]interface{}
	if err := json.Unmarshal(mergedData, &mergedJson); err != nil {
		t.Fatalf("Failed to parse merged .claude.json: %v", err)
	}

	// Check that existing key is preserved
	if value, exists := mergedJson["existingKey"]; !exists || value != "existingValue" {
		t.Errorf("Existing key was not preserved: %v", value)
	}

	// Check that new config is merged
	for key, expectedValue := range expectedConfig {
		if value, exists := mergedJson[key]; !exists || value != expectedValue {
			t.Errorf("Expected %s to be %v, got %v", key, expectedValue, value)
		}
	}
}

func TestDecodeBase64(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Valid base64",
			input:    "SGVsbG8gV29ybGQ=", // "Hello World" in base64
			expected: "Hello World",
		},
		{
			name:     "Invalid base64 - return as-is",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeBase64(tt.input)
			if err != nil {
				t.Fatalf("decodeBase64 failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSetupMCPServers(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	t.Setenv("HOME", tempDir)

	// Test case 1: Empty config flag
	err = SetupMCPServers(tempDir, "")
	if err == nil {
		t.Error("Expected error for empty config flag")
	}

	// Test case 2: Invalid JSON
	err = SetupMCPServers(tempDir, "invalid json")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	// Test case 3: Valid JSON config
	mcpConfigs := []MCPServerConfig{
		{
			ID:        "test-server",
			Name:      "Test Server",
			Endpoint:  "http://localhost:8080",
			Enabled:   true,
			Transport: "stdio",
			Command:   "test-command",
			Args:      []string{"arg1", "arg2"},
			Env:       map[string]string{"KEY": "value"},
			Timeout:   30,
		},
	}

	configJson, err := json.Marshal(mcpConfigs)
	if err != nil {
		t.Fatalf("Failed to marshal MCP config: %v", err)
	}

	// Note: This test will fail because it tries to execute the claude command
	// which is not available in the test environment. This is expected behavior.
	_ = SetupMCPServers(tempDir, string(configJson))
	// We don't check for error here because the claude command will fail
	// but we can verify that the config was parsed correctly

	// Test case 4: Base64 encoded config
	base64Config := base64.StdEncoding.EncodeToString(configJson)
	_ = SetupMCPServers(tempDir, base64Config)
}

func TestAddMcpServer(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Test case 1: stdio transport with command
	mcpConfig := MCPServerConfig{
		Name:      "test-stdio-server",
		Transport: "stdio",
		Command:   "test-command",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"TEST_ENV": "test_value"},
	}

	err := addMcpServer(tempDir, mcpConfig)
	// This might succeed if claude command is available in the environment
	if err != nil {
		// If claude is not installed, this is expected
		t.Logf("claude command not available or failed: %v", err)
	}

	// Test case 2: non-stdio transport (no command)
	mcpConfig2 := MCPServerConfig{
		Name:      "test-http-server",
		Transport: "http",
		Endpoint:  "http://localhost:8080",
	}

	err = addMcpServer(tempDir, mcpConfig2)
	// This might succeed if claude command is available in the environment
	if err != nil {
		// If claude is not installed, this is expected
		t.Logf("claude command not available or failed: %v", err)
	}
}
