package startup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected string
		hasError bool
	}{
		{
			name:     "HTTPS URL",
			repoURL:  "https://github.com/owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "HTTPS URL with .git",
			repoURL:  "https://github.com/owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "SSH URL",
			repoURL:  "git@github.com:owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "SSH URL without .git",
			repoURL:  "git@github.com:owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "GitHub Enterprise HTTPS",
			repoURL:  "https://github.enterprise.com/owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "GitHub Enterprise SSH",
			repoURL:  "git@github.enterprise.com:owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "Invalid URL",
			repoURL:  "invalid-url",
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractRepoName(tt.repoURL)
			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error for URL %s", tt.repoURL)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for URL %s: %v", tt.repoURL, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestGetGitHubURL(t *testing.T) {
	// Save original environment
	originalAPI := os.Getenv("GITHUB_API")
	defer func() {
		if originalAPI != "" {
			if err := os.Setenv("GITHUB_API", originalAPI); err != nil {
				t.Logf("Failed to restore GITHUB_API: %v", err)
			}
		} else {
			if err := os.Unsetenv("GITHUB_API"); err != nil {
				t.Logf("Failed to unset GITHUB_API: %v", err)
			}
		}
	}()

	// Test case 1: No GITHUB_API environment variable
	if err := os.Unsetenv("GITHUB_API"); err != nil {
		t.Fatalf("Failed to unset GITHUB_API: %v", err)
	}
	result := getGitHubURL()
	expected := "https://github.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 2: GITHUB_API with /api/v3 suffix
	if err := os.Setenv("GITHUB_API", "https://github.enterprise.com/api/v3"); err != nil {
		t.Fatalf("Failed to set GITHUB_API: %v", err)
	}
	result = getGitHubURL()
	expected = "https://github.enterprise.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 3: GITHUB_API without /api/v3 suffix
	if err := os.Setenv("GITHUB_API", "https://github.enterprise.com"); err != nil {
		t.Fatalf("Failed to set GITHUB_API: %v", err)
	}
	result = getGitHubURL()
	expected = "https://github.enterprise.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestGetGitHubToken(t *testing.T) {
	// Clear all environment variables
	unsetStartupTestEnv(t, "GITHUB_TOKEN")
	unsetStartupTestEnv(t, "GITHUB_PERSONAL_ACCESS_TOKEN")
	unsetStartupTestEnv(t, "GITHUB_APP_ID")
	unsetStartupTestEnv(t, "GITHUB_INSTALLATION_ID")
	unsetStartupTestEnv(t, "GITHUB_APP_PEM_PATH")
	unsetStartupTestEnv(t, "GITHUB_REPO_FULLNAME")

	// Test case 1: GITHUB_TOKEN environment variable
	t.Setenv("GITHUB_TOKEN", "test-token")
	token, err := GetGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_TOKEN: %v", err)
	}
	if token != "test-token" {
		t.Errorf("Expected test-token, got %s", token)
	}

	// Test case 2: GITHUB_PERSONAL_ACCESS_TOKEN fallback
	unsetStartupTestEnv(t, "GITHUB_TOKEN")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "personal-token")
	token, err = GetGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_PERSONAL_ACCESS_TOKEN: %v", err)
	}
	if token != "personal-token" {
		t.Errorf("Expected personal-token, got %s", token)
	}

	// Test case 3: GitHub App auth with manual installation ID
	unsetStartupTestEnv(t, "GITHUB_PERSONAL_ACCESS_TOKEN")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_INSTALLATION_ID", "67890")

	// Create a dummy PEM file
	tempDir := t.TempDir()
	pemFile := filepath.Join(tempDir, "test.pem")
	// Invalid PEM content will cause error, but it tests the code path
	if err := os.WriteFile(pemFile, []byte("invalid-pem"), 0600); err != nil {
		t.Fatalf("Failed to create test PEM file: %v", err)
	}
	t.Setenv("GITHUB_APP_PEM_PATH", pemFile)

	_, err = GetGitHubToken("")
	// This will fail because of invalid PEM, but tests the code path
	if err == nil {
		t.Error("Expected error with invalid PEM content")
	}

	// Test case 4: GitHub App auth with auto-discovery (no installation ID)
	unsetStartupTestEnv(t, "GITHUB_INSTALLATION_ID")
	t.Setenv("GITHUB_REPO_FULLNAME", "owner/repo")

	_, err = GetGitHubToken("")
	// This will fail because of invalid PEM, but tests the auto-discovery code path
	if err == nil {
		t.Error("Expected error with invalid PEM content in auto-discovery")
	}

	// Test case 5: GitHub App auth with repoFullName parameter
	unsetStartupTestEnv(t, "GITHUB_REPO_FULLNAME")
	_, err = GetGitHubToken("owner/repo")
	// This will fail because of invalid PEM, but tests the parameter path
	if err == nil {
		t.Error("Expected error with invalid PEM content with repoFullName parameter")
	}

	// Test case 6: No authentication available
	unsetStartupTestEnv(t, "GITHUB_APP_ID")
	unsetStartupTestEnv(t, "GITHUB_APP_PEM_PATH")
	_, err = GetGitHubToken("")
	if err == nil {
		t.Error("Expected error when no authentication is available")
	}
}

func TestMCPServerConfig(t *testing.T) {
	// Test MCPServerConfig struct marshaling/unmarshaling
	config := MCPServerConfig{
		ID:        "test-id",
		Name:      "Test MCP Server",
		Endpoint:  "http://localhost:8080",
		Enabled:   true,
		Transport: "stdio",
		Command:   "test-command",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"KEY1": "value1", "KEY2": "value2"},
		Timeout:   30,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal MCPServerConfig: %v", err)
	}

	// Unmarshal back
	var unmarshaled MCPServerConfig
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPServerConfig: %v", err)
	}

	// Compare structs
	if !reflect.DeepEqual(config, unmarshaled) {
		t.Errorf("Marshaled and unmarshaled configs don't match")
	}
}

func TestInitGitHubRepo(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "github-repo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Test case 1: Empty repo name with ignoreMissingConfig=true
	cloneDir := filepath.Join(tempDir, "test-repo")
	err = InitGitHubRepo("", cloneDir, true)
	if err != nil {
		t.Errorf("Expected no error with empty repo name and ignoreMissingConfig=true: %v", err)
	}

	// Test case 2: Empty repo name with ignoreMissingConfig=false
	err = InitGitHubRepo("", cloneDir, false)
	if err == nil {
		t.Error("Expected error with empty repo name and ignoreMissingConfig=false")
	}

	// Test case 3: Valid repo name but no authentication
	// This will fail because no GitHub token is available, which is expected
	err = InitGitHubRepo("owner/repo", cloneDir, false)
	if err == nil {
		t.Error("Expected error with no GitHub authentication")
	}
}

func TestGenerateGitHubAppToken(t *testing.T) {
	// Test case 1: Invalid app ID
	_, err := GenerateGitHubAppToken("invalid", "123", "/path/to/pem")
	if err == nil {
		t.Error("Expected error for invalid app ID")
	}

	// Test case 2: Invalid installation ID
	_, err = GenerateGitHubAppToken("123", "invalid", "/path/to/pem")
	if err == nil {
		t.Error("Expected error for invalid installation ID")
	}

	// Test case 3: Non-existent PEM file
	tempDir := t.TempDir()
	nonExistentPem := filepath.Join(tempDir, "non-existent.pem")
	_, err = GenerateGitHubAppToken("123", "456", nonExistentPem)
	if err == nil {
		t.Error("Expected error for non-existent PEM file")
	}

	// Test case 4: PEM file fallback to environment variable

	// Set a dummy PEM content
	t.Setenv("GITHUB_APP_PEM", "dummy-pem-content")
	_, err = GenerateGitHubAppToken("123", "456", nonExistentPem)
	// This will still fail because it's not a valid PEM, but it tests the env var fallback path
	if err == nil {
		t.Error("Expected error for invalid PEM content")
	}
}

func TestInstallationTokenRepositories(t *testing.T) {
	t.Run("no repository without restriction", func(t *testing.T) {
		t.Setenv("REPOSITORY_RESTRICTION", "")

		repositories, err := installationTokenRepositories("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repositories != nil {
			t.Fatalf("expected nil repositories, got %#v", repositories)
		}
	})

	t.Run("no repository with restriction", func(t *testing.T) {
		t.Setenv("REPOSITORY_RESTRICTION", "true")

		_, err := installationTokenRepositories("")
		if err == nil {
			t.Fatal("expected error when repository restriction is enabled")
		}
	})

	t.Run("repository fullname becomes token repository name", func(t *testing.T) {
		t.Setenv("REPOSITORY_RESTRICTION", "true")

		repositories, err := installationTokenRepositories("owner/repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(repositories, []string{"repo"}) {
			t.Fatalf("expected repository name, got %#v", repositories)
		}
	})

	t.Run("invalid repository fullname", func(t *testing.T) {
		t.Setenv("REPOSITORY_RESTRICTION", "")

		_, err := installationTokenRepositories("repo")
		if err == nil {
			t.Fatal("expected error for invalid repository fullname")
		}
	})
}
