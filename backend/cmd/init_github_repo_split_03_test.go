package cmd

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-github/v57/github"
	testassert "github.com/stretchr/testify/assert"
)

func TestSetupMCPIntegration(t *testing.T) {
	// Save original PATH
	originalPath := os.Getenv("PATH")

	tests := []struct {
		name        string
		token       string
		cloneDir    string
		setupMock   func(t *testing.T, tmpDir string)
		expectError bool
	}{
		{
			name:     "Successful MCP setup",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock claude executable
				mockClaude := `#!/bin/bash
if [ "$1" = "mcp" ] && [ "$2" = "add" ] && [ "$3" = "github" ]; then
  echo "✓ Added GitHub MCP server"
  exit 0
fi
exit 1`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				claudePath := mockPath + "/claude"
				testassert.NoError(t, os.WriteFile(claudePath, []byte(mockClaude), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false,
		},
		{
			name:     "MCP setup fails",
			token:    "test-token",
			cloneDir: "",
			setupMock: func(t *testing.T, tmpDir string) {
				// Create mock claude executable that fails
				mockClaude := `#!/bin/bash
echo "error: failed to add MCP server" >&2
exit 1`
				mockPath := tmpDir + "/mock-bin"
				testassert.NoError(t, os.MkdirAll(mockPath, 0755))
				claudePath := mockPath + "/claude"
				testassert.NoError(t, os.WriteFile(claudePath, []byte(mockClaude), 0755))
				t.Setenv("PATH", mockPath+":"+originalPath)
			},
			expectError: false, // setupMCPIntegration only warns on failure
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

			// Test setupMCPIntegration
			err := setupMCPIntegration(tt.token, tt.cloneDir)

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

func TestFindInstallationIDForRepo(t *testing.T) {
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

	tests := []struct {
		name          string
		appID         int64
		repoFullName  string
		apiBase       string
		serverHandler http.HandlerFunc
		expectedID    int64
		expectError   bool
		errorContains string
	}{
		{
			name:         "Found installation with access",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Log the request for debugging
				t.Logf("Request: %s %s", r.Method, r.URL.Path)

				switch r.URL.Path {
				case "/app/installations", "/api/v3/app/installations":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					// GitHub API returns array of Installation objects
					response := []*github.Installation{
						{ID: github.Int64(12345), Account: &github.User{Login: github.String("owner"), Type: github.String("Organization")}},
						{ID: github.Int64(67890), Account: &github.User{Login: github.String("other"), Type: github.String("User")}},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/installation/repositories", "/api/v3/installation/repositories":
					// Return list of repositories with permissions for the installation
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					response := map[string]interface{}{
						"total_count": 1,
						"repositories": []map[string]interface{}{
							{
								"id":        123456,
								"name":      "repo",
								"full_name": "owner/repo",
								"owner": map[string]interface{}{
									"login": "owner",
								},
								"permissions": map[string]bool{
									"admin": false,
									"push":  true,
									"pull":  true,
								},
							},
						},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/repos/owner/repo", "/api/v3/repos/owner/repo":
					// Return 200 for the first installation
					if r.Header.Get("Authorization") != "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						response := map[string]interface{}{
							"id":        123456,
							"name":      "repo",
							"full_name": "owner/repo",
							"owner": map[string]interface{}{
								"login": "owner",
							},
						}
						testassert.NoError(t, json.NewEncoder(w).Encode(response))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case "/app", "/user", "/api/v3/app", "/api/v3/user":
					// These endpoints are called during authentication
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				}
			},
			expectedID:  12345,
			expectError: false,
		},
		{
			name:         "No installation with access",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Log the request for debugging
				t.Logf("Request: %s %s", r.Method, r.URL.Path)

				switch r.URL.Path {
				case "/app/installations", "/api/v3/app/installations":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					// GitHub API returns array of Installation objects
					response := []*github.Installation{
						{ID: github.Int64(12345), Account: &github.User{Login: github.String("owner"), Type: github.String("Organization")}},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/installation/repositories", "/api/v3/installation/repositories":
					// Return empty list or read-only permissions
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					response := map[string]interface{}{
						"total_count": 1,
						"repositories": []map[string]interface{}{
							{
								"id":        123456,
								"name":      "repo",
								"full_name": "owner/repo",
								"owner": map[string]interface{}{
									"login": "owner",
								},
								"permissions": map[string]bool{
									"admin": false,
									"push":  false, // No write permission
									"pull":  true,
								},
							},
						},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/repos/owner/repo", "/api/v3/repos/owner/repo":
					// Return 404 for all installations
					w.WriteHeader(http.StatusNotFound)
					writeGitHubTestResponse(t, w, `{"message": "Not Found"}`)
				case "/app", "/user", "/api/v3/app", "/api/v3/user":
					// These endpoints are called during authentication
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				}
			},
			expectError:   true,
			errorContains: "no installation found with access",
		},
		{
			name:         "Installation with read-only access",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Log the request for debugging
				t.Logf("Request: %s %s", r.Method, r.URL.Path)

				switch r.URL.Path {
				case "/app/installations", "/api/v3/app/installations":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					// GitHub API returns array of Installation objects
					response := []*github.Installation{
						{ID: github.Int64(12345), Account: &github.User{Login: github.String("owner"), Type: github.String("Organization")}},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/installation/repositories", "/api/v3/installation/repositories":
					// Return repository with read-only permissions
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					response := map[string]interface{}{
						"total_count": 1,
						"repositories": []map[string]interface{}{
							{
								"id":        123456,
								"name":      "repo",
								"full_name": "owner/repo",
								"owner": map[string]interface{}{
									"login": "owner",
								},
								"permissions": map[string]bool{
									"admin": false,
									"push":  false, // No write permission
									"pull":  true,
								},
							},
						},
					}
					testassert.NoError(t, json.NewEncoder(w).Encode(response))
				case "/repos/owner/repo", "/api/v3/repos/owner/repo":
					// Return 200 - repository is accessible
					if r.Header.Get("Authorization") != "" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						response := map[string]interface{}{
							"id":        123456,
							"name":      "repo",
							"full_name": "owner/repo",
							"owner": map[string]interface{}{
								"login": "owner",
							},
						}
						testassert.NoError(t, json.NewEncoder(w).Encode(response))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case "/app", "/user", "/api/v3/app", "/api/v3/user":
					// These endpoints are called during authentication
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				default:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				}
			},
			expectedID:  12345,
			expectError: false, // Now accepts read-only installations
		},
		{
			name:         "Invalid repo fullname format",
			appID:        123,
			repoFullName: "invalid-format",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				writeGitHubTestResponse(t, w, `{}`)
			},
			expectError:   true,
			errorContains: "invalid repository fullname format",
		},
		{
			name:         "API error listing installations",
			appID:        123,
			repoFullName: "owner/repo",
			apiBase:      "",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app/installations" || r.URL.Path == "/api/v3/app/installations" {
					w.WriteHeader(http.StatusInternalServerError)
					writeGitHubTestResponse(t, w, `{"message": "Internal server error"}`)
				} else {
					w.WriteHeader(http.StatusOK)
					writeGitHubTestResponse(t, w, `{}`)
				}
			},
			expectError:   true,
			errorContains: "failed to list installations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before each test to ensure independence
			installationCache.ClearCache()

			// Create test server
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Use server URL as API base if not specified
			apiBase := tt.apiBase
			if apiBase == "" {
				apiBase = server.URL
			}

			// Test findInstallationIDForRepo
			result, err := findInstallationIDForRepo(tt.appID, pemFile, tt.repoFullName, apiBase)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expectedID {
					t.Errorf("Expected installation ID %d, got %d", tt.expectedID, result)
				}
			}
		})
	}
}

func writeGitHubTestResponse(t *testing.T, w http.ResponseWriter, payload string) {
	t.Helper()
	_, err := w.Write([]byte(payload))
	testassert.NoError(t, err)
}

// Helper functions
