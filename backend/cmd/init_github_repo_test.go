package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	testassert "github.com/stretchr/testify/assert"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

func TestGenerateInstallationToken(t *testing.T) {
	// Generate a test RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}

	// Create a temporary PEM file
	pemFile, err := createTempPEMFile(privateKey)
	if err != nil {
		t.Fatalf("Failed to create temp PEM file: %v", err)
	}
	t.Cleanup(func() { testassert.NoError(t, os.Remove(pemFile)) })

	// Mock GitHub API server that handles installation token requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle installation token request with API v3 prefix
		if r.URL.Path == "/api/v3/app/installations/12345/access_tokens" && r.Method == "POST" {
			auth := r.Header.Get("Authorization")
			if auth == "" || !startsWith(auth, "Bearer ") {
				t.Errorf("Expected Bearer token in Authorization header, got %s", auth)
			}

			w.WriteHeader(http.StatusCreated)
			response := map[string]interface{}{
				"token":      "test-installation-token",
				"expires_at": "2025-06-09T04:00:00Z",
			}
			testassert.NoError(t, json.NewEncoder(w).Encode(response))
			return
		}

		// For go-github library, it may make additional requests for JWT authentication
		if r.Method == "GET" && (r.URL.Path == "/api/v3/app" || r.URL.Path == "/api/v3/user") {
			w.WriteHeader(http.StatusOK)
			_, err := fmt.Fprint(w, `{}`)
			testassert.NoError(t, err)
			return
		}

		t.Logf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        pemFile,
		APIBase:        server.URL,
	}

	token, err := generateInstallationToken(config)
	if err != nil {
		t.Fatalf("generateInstallationToken failed: %v", err)
	}

	if token != "test-installation-token" {
		t.Errorf("Expected token 'test-installation-token', got '%s'", token)
	}
}

func TestGenerateInstallationTokenWithInvalidPEM(t *testing.T) {
	// Create a temporary invalid PEM file
	tmpFile, err := os.CreateTemp("", "invalid-key-*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	t.Cleanup(func() { testassert.NoError(t, os.Remove(tmpFile.Name())) })

	_, err = tmpFile.WriteString("invalid pem content")
	testassert.NoError(t, err)
	testassert.NoError(t, tmpFile.Close())

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        tmpFile.Name(),
		APIBase:        "https://api.github.com",
	}

	token, err := generateInstallationToken(config)
	testassert.Error(t, err)
	testassert.Empty(t, token)
}

func TestGenerateInstallationTokenWithAPIError(t *testing.T) {
	// Generate a test RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test private key: %v", err)
	}

	// Create a temporary PEM file
	pemFile, err := createTempPEMFile(privateKey)
	if err != nil {
		t.Fatalf("Failed to create temp PEM file: %v", err)
	}
	t.Cleanup(func() { testassert.NoError(t, os.Remove(pemFile)) })

	// Mock GitHub API server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, err := fmt.Fprint(w, `{"message":"Bad credentials"}`)
		testassert.NoError(t, err)
	}))
	defer server.Close()

	config := GitHubAppConfig{
		AppID:          123,
		InstallationID: 12345,
		PEMPath:        pemFile,
		APIBase:        server.URL,
	}

	token, err := generateInstallationToken(config)
	testassert.Error(t, err)
	testassert.Empty(t, token)
}

func TestCreateAuthenticatedURL(t *testing.T) {
	tests := []struct {
		name        string
		repoURL     string
		token       string
		githubURL   string
		expected    string
		expectError bool
	}{
		{
			name:      "HTTPS URL",
			repoURL:   "https://github.com/user/repo",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo",
		},
		{
			name:      "SSH URL",
			repoURL:   "git@github.com:user/repo.git",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo.git",
		},
		{
			name:      "SSH URL without .git",
			repoURL:   "git@github.com:user/repo",
			token:     "test-token",
			githubURL: "https://github.com",
			expected:  "https://test-token@github.com/user/repo.git",
		},
		{
			name:      "Enterprise GitHub HTTPS URL",
			repoURL:   "https://github.enterprise.com/user/repo",
			token:     "test-token",
			githubURL: "https://github.enterprise.com",
			expected:  "https://test-token@github.enterprise.com/user/repo",
		},
		{
			name:      "Enterprise GitHub SSH URL",
			repoURL:   "git@github.enterprise.com:user/repo.git",
			token:     "test-token",
			githubURL: "https://github.enterprise.com",
			expected:  "https://test-token@github.enterprise.com/user/repo.git",
		},
		{
			name:        "Invalid URL",
			repoURL:     "invalid-url",
			token:       "test-token",
			githubURL:   "https://github.com",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set GITHUB_URL environment variable
			t.Setenv("GITHUB_URL", tt.githubURL)

			result, err := github_pkg.CreateAuthenticatedURL(tt.repoURL, tt.token)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetAPIBase(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default API base",
			envValue: "",
			expected: "https://api.github.com",
		},
		{
			name:     "Custom API base",
			envValue: "https://github.enterprise.com/api/v3",
			expected: "https://github.enterprise.com/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			t.Setenv("GITHUB_API", tt.envValue)

			result := getAPIBase()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default GitHub URL",
			envValue: "",
			expected: "https://github.com",
		},
		{
			name:     "Custom GitHub URL",
			envValue: "https://github.enterprise.com",
			expected: "https://github.enterprise.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			t.Setenv("GITHUB_URL", tt.envValue)

			result := getGitHubURL()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSaveEnvironmentVariables(t *testing.T) {
	token := "test-token"
	cloneDir := "/test/path"

	err := saveEnvironmentVariables(token, cloneDir)
	if err != nil {
		t.Fatalf("saveEnvironmentVariables failed: %v", err)
	}

	// Check if the file was created
	pid := os.Getpid()
	envFile := fmt.Sprintf("/tmp/github_env_%d", pid)
	t.Cleanup(func() { testassert.NoError(t, os.Remove(envFile)) })

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("Failed to read environment file: %v", err)
	}

	expectedContent := fmt.Sprintf("GITHUB_TOKEN=%s\nGITHUB_CLONE_DIR=%s\n", token, cloneDir)
	if string(content) != expectedContent {
		t.Errorf("Expected content:\n%s\nGot:\n%s", expectedContent, string(content))
	}

	// Check file permissions
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("Failed to stat environment file: %v", err)
	}

	expectedMode := os.FileMode(0600)
	if info.Mode() != expectedMode {
		t.Errorf("Expected file mode %o, got %o", expectedMode, info.Mode())
	}
}

func TestRunInitGitHubRepoValidation(t *testing.T) {
	// Test missing GITHUB_REPO_FULLNAME
	t.Setenv("GITHUB_REPO_FULLNAME", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_INSTALLATION_ID", "")
	t.Setenv("GITHUB_APP_PEM_PATH", "")

	err := RunInitGitHubRepo("", "", false)
	if err == nil || !contains(err.Error(), "repository fullname is required") {
		t.Errorf("Expected error about missing repository fullname, got: %v", err)
	}

	// Test missing authentication
	err = RunInitGitHubRepo("test/repo", "", false)
	if err == nil || !contains(err.Error(), "GITHUB_APP_ID") {
		t.Errorf("Expected error about invalid GITHUB_APP_ID, got: %v", err)
	}
}

func TestParseAppID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "Valid app ID",
			input:    "123",
			expected: 123,
		},
		{
			name:     "Large app ID",
			input:    "123456789",
			expected: 123456789,
		},
		{
			name:        "Empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "Invalid number",
			input:       "abc",
			expectError: true,
		},
		{
			name:     "Negative number",
			input:    "-123",
			expected: -123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseAppID(tt.input)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}
