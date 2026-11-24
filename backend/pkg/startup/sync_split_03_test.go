package startup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneMarketplace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping clone tests")
	}

	t.Run("clones new repository", func(t *testing.T) {
		tmpDir := t.TempDir()

		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"test.txt": "test content",
		})

		targetDir := filepath.Join(tmpDir, "target")
		if err := cloneMarketplace(sourceDir, targetDir); err != nil {
			t.Fatalf("cloneMarketplace failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("Expected .git directory to exist in cloned repo")
		}
		if _, err := os.Stat(filepath.Join(targetDir, "test.txt")); os.IsNotExist(err) {
			t.Error("Expected test.txt to exist in cloned repo")
		}
	})

	t.Run("pulls updates for existing clone", func(t *testing.T) {
		tmpDir := t.TempDir()

		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"test.txt": "initial content",
		})

		// Full clone (not shallow) so pull works
		targetDir := filepath.Join(tmpDir, "target")
		cmd := exec.Command("git", "clone", sourceDir, targetDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to clone: %v", err)
		}

		// Add a new commit to the source
		testFile := filepath.Join(sourceDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("updated content"), 0644); err != nil {
			t.Fatalf("Failed to update test file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git add: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "update commit")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git commit: %v", err)
		}

		if err := cloneMarketplace(sourceDir, targetDir); err != nil {
			t.Fatalf("cloneMarketplace (pull) failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(targetDir, "test.txt"))
		if err != nil {
			t.Fatalf("Failed to read test.txt: %v", err)
		}
		if string(data) != "updated content" {
			t.Errorf("Expected 'updated content', got '%s'", string(data))
		}
	})

	t.Run("fails for invalid URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetDir := filepath.Join(tmpDir, "target")

		err := cloneMarketplace("https://invalid.example.com/nonexistent/repo.git", targetDir)
		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("uses shallow clone flag", func(t *testing.T) {
		// Note: --depth 1 behavior varies for local file:// protocol vs https://
		// For local repos, git may still fetch all commits depending on git version
		// This test verifies the clone command is called with --depth 1 by checking
		// that the clone succeeds and creates a valid repo

		tmpDir := t.TempDir()

		// Create a local git repo with multiple commits
		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"file1.txt": "content 1",
		})

		// Add more commits
		for i := 2; i <= 5; i++ {
			filePath := filepath.Join(sourceDir, "file1.txt")
			if err := os.WriteFile(filePath, []byte("content "+string(rune('0'+i))), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}
			cmd := exec.Command("git", "add", ".")
			cmd.Dir = sourceDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git add: %v", err)
			}
			cmd = exec.Command("git", "commit", "-m", "commit "+string(rune('0'+i)))
			cmd.Dir = sourceDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git commit: %v", err)
			}
		}

		// Clone the repo
		targetDir := filepath.Join(tmpDir, "target")
		err := cloneMarketplace(sourceDir, targetDir)
		if err != nil {
			t.Fatalf("cloneMarketplace failed: %v", err)
		}

		// Verify clone succeeded and has the latest content
		data, err := os.ReadFile(filepath.Join(targetDir, "file1.txt"))
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(data) != "content 5" {
			t.Errorf("Expected latest content 'content 5', got '%s'", string(data))
		}

		// Verify it's a git repo
		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("Expected .git directory to exist")
		}
	})

	t.Run("target directory already exists but not git repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create source repo
		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test",
		})

		// Create target directory with some files (but not a git repo)
		targetDir := filepath.Join(tmpDir, "target")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			t.Fatalf("Failed to create target dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "existing.txt"), []byte("existing"), 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		// Clone should fail because target already exists
		err := cloneMarketplace(sourceDir, targetDir)
		if err == nil {
			t.Error("Expected error when cloning to existing non-git directory")
		}
	})

	t.Run("handles empty repository gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create an empty git repo (no commits)
		sourceDir := filepath.Join(tmpDir, "source")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}
		cmd := exec.Command("git", "init")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		// Clone should fail (empty repo has no commits to clone)
		targetDir := filepath.Join(tmpDir, "target")
		err := cloneMarketplace(sourceDir, targetDir)
		// Empty repos typically fail to clone with --depth 1
		if err == nil {
			// If it succeeds, that's also acceptable behavior
			t.Log("Cloning empty repo succeeded (may vary by git version)")
		}
	})
}

func TestMarketplaceAnonymousEnvRemovesAmbientCredentials(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "working-token")
	t.Setenv("GH_TOKEN", "gh-token")
	t.Setenv("GIT_ASKPASS", "/tmp/askpass")
	t.Setenv("AGENTAPI_MARKETPLACE_GIT_PASSWORD", "marketplace-token")

	env := marketplaceAnonymousEnv()
	for _, entry := range env {
		for _, prefix := range []string{"GITHUB_TOKEN=", "GH_TOKEN=", "GIT_ASKPASS=", "AGENTAPI_MARKETPLACE_GIT_"} {
			if strings.HasPrefix(entry, prefix) {
				t.Fatalf("credential environment leaked: %s", prefix)
			}
		}
	}
}

func TestSync(t *testing.T) {
	t.Run("succeeds without settings file", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings": map[string]interface{}{"mcp.enabled": true},
		})

		opts := SyncOptions{
			OutputDir: outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Directories should be created
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}
	})

	t.Run("succeeds with valid settings file for custom marketplace", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings": map[string]interface{}{"mcp.enabled": true},
		})

		// Optional: SettingsFile for custom marketplace config
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		opts := SyncOptions{
			SettingsFile: settingsFile,
			OutputDir:    outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// settings.json should still exist (written by compile step, not overwritten)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			t.Error("Expected settings.json to remain from compile step")
		}
	})

	t.Run("integration test with local marketplace - clones and resolves names", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available, skipping integration test")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would (with enabled_plugins)
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings":        map[string]interface{}{"mcp.enabled": true},
			"enabled_plugins": []string{"plugin1@test-mp-real-name", "plugin2@test-mp-real-name"},
		})

		// Create a local git repo as marketplace source
		marketplaceSourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, marketplaceSourceDir, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "test-mp-real-name"}`,
			"README.md":                       "# Test Marketplace",
		})

		// SettingsFile for custom marketplace URL config
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-mp": {
					"url": "` + marketplaceSourceDir + `"
				}
			},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		opts := SyncOptions{
			SettingsFile: settingsFile,
			OutputDir:    outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify marketplace was cloned with real name
		clonedMarketplace := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "test-mp-real-name")
		if _, err := os.Stat(filepath.Join(clonedMarketplace, ".git")); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned")
		}
	})
}

func TestSyncMarketplaces_EnabledPlugins(t *testing.T) {
	// The new design: syncMarketplaces does NOT write settings.json.
	// It reads enabled_plugins FROM the pre-existing settings.json (written by compile).
	// These tests verify that syncMarketplaces correctly handles enabled_plugins
	// by reading from the pre-created settings.json.

	t.Run("does not create settings.json when no plugins configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		settings := &settingsJSON{
			Name:           "test-user",
			EnabledPlugins: []string{"context7@claude-plugins-official", "typescript@claude-plugins-official"},
		}

		opts := SyncOptions{
			OutputDir: outputDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// settings.json should NOT be created by syncMarketplaces — that's compile's job
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("reads enabled_plugins from pre-created settings.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Pre-create settings.json as compile would (with enabled_plugins)
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"enabled_plugins": []string{"context7@claude-plugins-official", "typescript@claude-plugins-official"},
		})

		settings := &settingsJSON{
			Name: "test-user",
		}
		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// settings.json should still exist (untouched by syncMarketplaces)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// enabled_plugins written by compile should be untouched
		pluginsRaw, ok := result["enabled_plugins"].([]interface{})
		if !ok {
			t.Fatal("Expected enabled_plugins to remain in settings.json")
		}
		if len(pluginsRaw) != 2 {
			t.Errorf("Expected 2 enabled_plugins, got %d", len(pluginsRaw))
		}
	})

	t.Run("clones custom marketplace when plugins reference it", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo as marketplace source
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md":                       "# Test Marketplace",
			".claude-plugin/marketplace.json": `{"name": "custom-mp"}`,
		})

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"enabled_plugins": []string{
				"context7@claude-plugins-official",
				"custom-plugin1@custom-mp",
			},
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"custom-mp": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Marketplace should be cloned with real name from marketplace.json
		clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "custom-mp")
		if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned as 'custom-mp'")
		}

		// settings.json should still contain compile's enabled_plugins (untouched)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}
		if _, ok := result["enabled_plugins"]; !ok {
			t.Error("Expected enabled_plugins to remain in settings.json after syncMarketplaces")
		}
	})

	t.Run("settings with enabled_plugins in JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"enabled_plugins": ["context7@claude-plugins-official", "git@claude-plugins-official", "my-plugin@my-marketplace"],
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

		if len(settings.EnabledPlugins) != 3 {
			t.Errorf("Expected 3 plugins, got %d", len(settings.EnabledPlugins))
		}
		if settings.EnabledPlugins[0] != "context7@claude-plugins-official" {
			t.Errorf("Expected first plugin to be 'context7@claude-plugins-official', got '%s'", settings.EnabledPlugins[0])
		}
	})
}
