package cmd

import (
	"os"
	"testing"

	testassert "github.com/stretchr/testify/assert"
)

func TestParseInstallationID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int64
		expectError bool
	}{
		{
			name:     "Valid installation ID",
			input:    "12345",
			expected: 12345,
		},
		{
			name:     "Large installation ID",
			input:    "987654321",
			expected: 987654321,
		},
		{
			name:        "Empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "Invalid number",
			input:       "xyz",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInstallationID(tt.input)

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

func TestSetupRepository(t *testing.T) {
	// Save original PATH for restoration
	originalPath := os.Getenv("PATH")

	tests := []struct {
		name        string
		repoURL     string
		token       string
		cloneDir    string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
		errorMsg    string
	}{
		{
			name:     "Clone new repository success",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "Fetched"
    ;;
  "checkout")
    if [ "$2" = "main" ]; then
      echo "Switched to branch 'main'"
      exit 0
    elif [ "$2" = "master" ]; then
      echo "Switched to branch 'master'"
      exit 0
    fi
    exit 1
    ;;
  *)
    echo "Unknown git command: $1"
    exit 1
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				gitPath := mockPath + "/git"
				testassert.NoError(t, os.WriteFile(gitPath, []byte(mockGit), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "Pull existing repository success",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create .git directory to simulate existing repo
				testassert.NoError(t, os.MkdirAll(tmpDir+"/.git", 0755))

				// Create mock git executable
				mockGit := `#!/bin/bash
if [ "$1" = "pull" ]; then
  echo "Already up to date."
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				gitPath := mockPath + "/git"
				testassert.NoError(t, os.WriteFile(gitPath, []byte(mockGit), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "Git init fails",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on init
				mockGit := `#!/bin/bash
if [ "$1" = "init" ]; then
  echo "fatal: cannot create directory" >&2
  exit 1
fi
exit 0`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				gitPath := mockPath + "/git"
				testassert.NoError(t, os.WriteFile(gitPath, []byte(mockGit), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to initialize git",
		},
		{
			name:     "Git fetch fails",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on fetch
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "fatal: could not read from remote repository" >&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				gitPath := mockPath + "/git"
				testassert.NoError(t, os.WriteFile(gitPath, []byte(mockGit), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to fetch",
		},
		{
			name:     "No main or master branch",
			repoURL:  "https://github.com/test/repo",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock git executable that fails on checkout
				mockGit := `#!/bin/bash
case "$1" in
  "init")
    mkdir -p .git
    ;;
  "remote")
    if [ "$2" = "add" ]; then
      echo "Remote added"
    fi
    ;;
  "fetch")
    echo "Fetched"
    ;;
  "checkout")
    echo "error: pathspec '$2' did not match any file(s) known to git" >&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
exit 0`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				gitPath := mockPath + "/git"
				testassert.NoError(t, os.WriteFile(gitPath, []byte(mockGit), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
			errorMsg:    "failed to checkout main or master branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()

			// Use tmpDir as cloneDir if not specified
			if tt.cloneDir == "" {
				tt.cloneDir = tmpDir
			}

			// Setup mock
			if tt.setupMock != nil {
				tt.setupMock(t, tmpDir)
			}

			// Test setupRepository
			err := setupRepository(tt.repoURL, tt.token, tt.cloneDir)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTTPS URL",
			input:    "https://github.com",
			expected: "github.com",
		},
		{
			name:     "HTTP URL",
			input:    "http://github.com",
			expected: "github.com",
		},
		{
			name:     "URL with path",
			input:    "https://github.com/api/v3",
			expected: "github.com",
		},
		{
			name:     "URL with port",
			input:    "https://github.enterprise.com:8080",
			expected: "github.enterprise.com:8080",
		},
		{
			name:     "Just hostname",
			input:    "github.com",
			expected: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHostname(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestAuthenticateGHCLI(t *testing.T) {
	// Save original PATH
	originalPath := os.Getenv("PATH")

	tests := []struct {
		name        string
		githubURL   string
		token       string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
	}{
		{
			name:      "Successful authentication",
			githubURL: "https://github.enterprise.com",
			token:     "test-token",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock gh executable
				mockGH := `#!/bin/bash
if [ "$1" = "auth" ] && [ "$2" = "login" ]; then
  # Read token from stdin
  read token
  echo "✓ Logged in to github.enterprise.com"
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				ghPath := mockPath + "/gh"
				testassert.NoError(t, os.WriteFile(ghPath, []byte(mockGH), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:      "Authentication failure",
			githubURL: "https://github.enterprise.com",
			token:     "invalid-token",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock gh executable that fails
				mockGH := `#!/bin/bash
if [ "$1" = "auth" ] && [ "$2" = "login" ]; then
  echo "error: authentication failed" >&2
  exit 1
fi
exit 0`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				ghPath := mockPath + "/gh"
				testassert.NoError(t, os.WriteFile(ghPath, []byte(mockGH), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()

			// Setup mock
			if tt.setupMock != nil {
				tt.setupMock(t, tmpDir)
			}

			// Test authenticateGHCLI
			err := authenticateGHCLI(tt.githubURL, tt.token)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
