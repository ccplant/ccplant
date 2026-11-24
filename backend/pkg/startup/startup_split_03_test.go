package startup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAutoDiscoverInstallationID(t *testing.T) {
	// Test case 1: Invalid repository format
	installationID, err := AutoDiscoverInstallationID("123", "/path/to/pem", "invalid-repo-format")
	if installationID != "" {
		t.Errorf("unexpected installation ID %q", installationID)
	}
	if err == nil {
		t.Error("Expected error for invalid repository format")
	}

	// Test case 2: Invalid app ID
	installationID, err = AutoDiscoverInstallationID("invalid", "/path/to/pem", "owner/repo")
	if installationID != "" {
		t.Errorf("unexpected installation ID %q", installationID)
	}
	if err == nil {
		t.Error("Expected error for invalid app ID")
	}

	// Test case 3: Non-existent PEM file without env var
	tempDir := t.TempDir()
	nonExistentPem := filepath.Join(tempDir, "non-existent.pem")

	t.Setenv("GITHUB_APP_PEM", "")

	installationID, err = AutoDiscoverInstallationID("123", nonExistentPem, "owner/repo")
	if installationID != "" {
		t.Errorf("unexpected installation ID %q", installationID)
	}
	if err == nil {
		t.Error("Expected error for non-existent PEM file")
	}
}

func TestAuthenticateGHCLI(t *testing.T) {
	// This function executes gh auth login command
	// We can only test that it attempts to run the command

	// Test with mock environment
	env := []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}
	err := AuthenticateGHCLI("github.enterprise.com", "test-token", env)
	// This will fail because gh command might not be available in test environment
	// but we're testing that the function attempts to execute it
	if err == nil {
		// If gh is installed, it would fail with auth error
		t.Log("gh command executed (unexpected in test environment)")
	}
}

func TestWithoutGitHubTokenEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin:/bin",
		"GITHUB_TOKEN=github-token",
		"GH_TOKEN=gh-token",
		"GITHUB_APP_ID=12345",
		"HOME=/tmp",
	}

	filtered := withoutGitHubTokenEnv(env)

	expected := []string{
		"PATH=/usr/bin:/bin",
		"GITHUB_APP_ID=12345",
		"HOME=/tmp",
	}
	if !reflect.DeepEqual(filtered, expected) {
		t.Fatalf("unexpected filtered env: got %#v, want %#v", filtered, expected)
	}
}

func TestNormalizeGitHubAuthStep(t *testing.T) {
	tests := []struct {
		name string
		step string
		want string
	}{
		{name: "empty defaults to all", step: "", want: "all"},
		{name: "all", step: "all", want: "all"},
		{name: "get token", step: "get-token", want: "get-token"},
		{name: "token alias", step: "token", want: "get-token"},
		{name: "auth login", step: "auth-login", want: "auth-login"},
		{name: "login alias", step: "login", want: "auth-login"},
		{name: "setup git", step: "setup-git", want: "setup-git"},
		{name: "git alias", step: "git", want: "setup-git"},
		{name: "trims and lowercases", step: " SETUP-GIT ", want: "setup-git"},
		{name: "invalid", step: "unknown", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeGitHubAuthStep(tt.step); got != tt.want {
				t.Fatalf("normalizeGitHubAuthStep(%q) = %q, want %q", tt.step, got, tt.want)
			}
		})
	}
}

func TestSetupRepository(t *testing.T) {
	// Test case 1: Invalid repository URL
	tempDir := t.TempDir()
	err := setupRepository("", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error for empty repository URL")
	}

	// Test case 2: Repository URL without proper format
	err = setupRepository("not-a-url", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error for invalid repository URL")
	}

	// Test case 3: Git repository detection (mock .git directory)
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git directory: %v", err)
	}

	// This will try to run git pull, which will fail in test environment
	err = setupRepository("https://github.com/owner/repo", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error when running git pull in test environment")
	}

	// Test case 4: GitHub Enterprise with GITHUB_API env var
	t.Setenv("GITHUB_API", "https://github.enterprise.com/api/v3")
	// Try to clone with enterprise URL
	err = setupRepository("https://github.enterprise.com/owner/repo", "test-token", tempDir)
	// This will fail but tests the enterprise path
	if err == nil {
		t.Error("Expected error when running gh repo clone in test environment")
	}
}
