package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Helper function to create a temp git repo for testing
func createTestGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to config git email: %v", err)
	}
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to config git name: %v", err)
	}

	for filename, content := range files {
		filePath := filepath.Join(dir, filename)
		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatalf("Failed to create parent directory for %s: %v", filename, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", filename, err)
		}
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}
}

func TestLoadSettingsFileValid(t *testing.T) {
	t.Run("valid settings file", func(t *testing.T) {
		// Create temp file with valid settings
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-marketplace": {
					"url": "https://github.com/example/marketplace"
				}
			},
			"enabled_plugins": ["plugin1@test-marketplace", "plugin2@test-marketplace"],
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "test-user" {
			t.Errorf("Expected name 'test-user', got '%s'", settings.Name)
		}
		if len(settings.Marketplaces) != 1 {
			t.Errorf("Expected 1 marketplace, got %d", len(settings.Marketplaces))
		}
		mp, ok := settings.Marketplaces["test-marketplace"]
		if !ok {
			t.Fatal("Expected 'test-marketplace' to exist")
		}
		if mp.URL != "https://github.com/example/marketplace" {
			t.Errorf("Expected URL 'https://github.com/example/marketplace', got '%s'", mp.URL)
		}
		if len(settings.EnabledPlugins) != 2 {
			t.Errorf("Expected 2 enabled plugins, got %d", len(settings.EnabledPlugins))
		}
	})
}

func TestLoadSettingsFileErrors(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		_, err := loadSettingsFile("")
		if err == nil {
			t.Error("Expected error for empty path")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := loadSettingsFile("/non/existent/path/settings.json")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte("not valid json"), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})
}

func TestLoadSettingsFileExtendedSettings(t *testing.T) {
	t.Run("settings with bedrock and mcp_servers", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"bedrock": {
				"enabled": true,
				"model": "anthropic.claude-3-opus-20240229-v1:0"
			},
			"mcp_servers": {
				"github": {
					"type": "sse",
					"url": "https://mcp.example.com"
				}
			},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Bedrock == nil {
			t.Fatal("Expected bedrock to be set")
		}
		if !settings.Bedrock.Enabled {
			t.Error("Expected bedrock.enabled to be true")
		}
		if settings.MCPServers == nil {
			t.Fatal("Expected mcp_servers to be set")
		}
		if _, ok := settings.MCPServers["github"]; !ok {
			t.Error("Expected 'github' MCP server to exist")
		}
	})
}

func TestLoadSettingsFileEmptyAndMinimal(t *testing.T) {
	t.Run("empty JSON object", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte("{}"), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "" {
			t.Errorf("Expected empty name, got '%s'", settings.Name)
		}
		if settings.Marketplaces != nil {
			t.Error("Expected nil marketplaces")
		}
	})

	t.Run("minimal settings with name only", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{"name": "minimal-user"}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "minimal-user" {
			t.Errorf("Expected name 'minimal-user', got '%s'", settings.Name)
		}
	})
}

func TestLoadSettingsFileMultipleMarketplaces(t *testing.T) {
	t.Run("multiple marketplaces", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"marketplace1": {
					"url": "https://github.com/org1/marketplace1"
				},
				"marketplace2": {
					"url": "https://github.com/org2/marketplace2"
				},
				"marketplace3": {
					"url": "https://github.com/org3/marketplace3"
				}
			},
			"enabled_plugins": ["plugin-a@marketplace1", "plugin-b@marketplace2", "plugin-c@marketplace2"]
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if len(settings.Marketplaces) != 3 {
			t.Errorf("Expected 3 marketplaces, got %d", len(settings.Marketplaces))
		}

		mp1 := settings.Marketplaces["marketplace1"]
		if mp1 == nil {
			t.Error("marketplace1 should exist")
		}

		mp2 := settings.Marketplaces["marketplace2"]
		if mp2 == nil {
			t.Error("marketplace2 should exist")
		}

		mp3 := settings.Marketplaces["marketplace3"]
		if mp3 == nil {
			t.Error("marketplace3 should exist")
		}

		if len(settings.EnabledPlugins) != 3 {
			t.Errorf("Expected 3 enabled plugins, got %d", len(settings.EnabledPlugins))
		}
	})
}

func TestLoadSettingsFileBedrock(t *testing.T) {
	t.Run("full bedrock configuration", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "bedrock-user",
			"bedrock": {
				"enabled": true,
				"model": "anthropic.claude-3-sonnet-20240229-v1:0",
				"access_key_id": "AKIAIOSFODNN7EXAMPLE",
				"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				"role_arn": "arn:aws:iam::123456789012:role/ExampleRole",
				"profile": "default"
			}
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Bedrock == nil {
			t.Fatal("Expected bedrock to be set")
		}
		if settings.Bedrock.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
			t.Errorf("Expected model, got '%s'", settings.Bedrock.Model)
		}
		if settings.Bedrock.AccessKeyID != "AKIAIOSFODNN7EXAMPLE" {
			t.Error("Expected access_key_id")
		}
		if settings.Bedrock.SecretAccessKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
			t.Error("Expected secret_access_key")
		}
		if settings.Bedrock.RoleARN != "arn:aws:iam::123456789012:role/ExampleRole" {
			t.Error("Expected role_arn")
		}
		if settings.Bedrock.Profile != "default" {
			t.Error("Expected profile")
		}
	})
}

func TestLoadSettingsFileMCPServers(t *testing.T) {
	t.Run("MCP server with all fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "mcp-user",
			"mcp_servers": {
				"custom-server": {
					"type": "stdio",
					"command": "/usr/bin/mcp-server",
					"args": ["--port", "8080", "--verbose"],
					"env": {
						"MCP_DEBUG": "true",
						"MCP_LOG_LEVEL": "debug"
					}
				},
				"sse-server": {
					"type": "sse",
					"url": "https://mcp.example.com/sse",
					"headers": {
						"Authorization": "Bearer token123",
						"X-Custom-Header": "value"
					}
				}
			}
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if len(settings.MCPServers) != 2 {
			t.Errorf("Expected 2 MCP servers, got %d", len(settings.MCPServers))
		}

		customServer := settings.MCPServers["custom-server"]
		if customServer == nil {
			t.Fatal("Expected custom-server to exist")
		}
		if customServer.Type != "stdio" {
			t.Errorf("Expected type 'stdio', got '%s'", customServer.Type)
		}
		if customServer.Command != "/usr/bin/mcp-server" {
			t.Errorf("Expected command, got '%s'", customServer.Command)
		}
		if len(customServer.Args) != 3 {
			t.Errorf("Expected 3 args, got %d", len(customServer.Args))
		}
		if len(customServer.Env) != 2 {
			t.Errorf("Expected 2 env vars, got %d", len(customServer.Env))
		}

		sseServer := settings.MCPServers["sse-server"]
		if sseServer == nil {
			t.Fatal("Expected sse-server to exist")
		}
		if sseServer.Type != "sse" {
			t.Errorf("Expected type 'sse', got '%s'", sseServer.Type)
		}
		if len(sseServer.Headers) != 2 {
			t.Errorf("Expected 2 headers, got %d", len(sseServer.Headers))
		}
	})
}

func TestLoadSettingsFileTruncatedAndEmpty(t *testing.T) {
	t.Run("truncated JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		// Truncated JSON
		if err := os.WriteFile(settingsFile, []byte(`{"name": "test`), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for truncated JSON")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte(""), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for empty file")
		}
	})
}
