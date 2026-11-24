package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMarketplaceName(t *testing.T) {
	t.Run("reads name from valid marketplace.json", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create .claude-plugin directory and marketplace.json
		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"name": "my-awesome-marketplace", "description": "A test marketplace"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		name, err := readMarketplaceName(tmpDir)
		if err != nil {
			t.Fatalf("readMarketplaceName failed: %v", err)
		}

		if name != "my-awesome-marketplace" {
			t.Errorf("Expected name 'my-awesome-marketplace', got '%s'", name)
		}
	})

	t.Run("returns error when marketplace.json not found", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when marketplace.json does not exist")
		}
	})

	t.Run("returns error when name field is empty", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"name": "", "description": "A test marketplace"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when name field is empty")
		}
	})

	t.Run("returns error when marketplace.json is invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte("not valid json"), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("returns error when name field is missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"description": "A test marketplace without name"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when name field is missing")
		}
	})
}

func TestResolvePluginName(t *testing.T) {
	t.Run("resolves plugin name with matching alias", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias":    "real-marketplace-name",
			"other-alias": "other-real-name",
		}

		result := resolvePluginName("my-plugin@my-alias", nameMapping)
		if result != "my-plugin@real-marketplace-name" {
			t.Errorf("Expected 'my-plugin@real-marketplace-name', got '%s'", result)
		}
	})

	t.Run("preserves plugin name when no matching alias", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias": "real-marketplace-name",
		}

		result := resolvePluginName("my-plugin@claude-plugins-official", nameMapping)
		if result != "my-plugin@claude-plugins-official" {
			t.Errorf("Expected 'my-plugin@claude-plugins-official', got '%s'", result)
		}
	})

	t.Run("preserves plugin without @ symbol", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias": "real-marketplace-name",
		}

		result := resolvePluginName("my-plugin", nameMapping)
		if result != "my-plugin" {
			t.Errorf("Expected 'my-plugin', got '%s'", result)
		}
	})

	t.Run("handles empty name mapping", func(t *testing.T) {
		nameMapping := map[string]string{}

		result := resolvePluginName("my-plugin@my-marketplace", nameMapping)
		if result != "my-plugin@my-marketplace" {
			t.Errorf("Expected 'my-plugin@my-marketplace', got '%s'", result)
		}
	})

	t.Run("handles plugin name with multiple @ symbols", func(t *testing.T) {
		nameMapping := map[string]string{
			"email@domain.com": "real-name",
		}

		// Only split on first @
		result := resolvePluginName("plugin@email@domain.com", nameMapping)
		if result != "plugin@real-name" {
			t.Errorf("Expected 'plugin@real-name', got '%s'", result)
		}
	})
}

func TestSyncMarketplaces_WithRealMarketplaceName(t *testing.T) {
	// The new design: syncMarketplaces reads the real marketplace name from
	// .claude-plugin/marketplace.json and clones into that directory name.
	// It does NOT write settings.json.

	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	t.Run("clones marketplace under real name from marketplace.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo as marketplace source with .claude-plugin/marketplace.json
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		mpJSON := `{"name": "jlaswell-community-marketplace", "description": "A community marketplace"}`
		createTestGitRepo(t, sourceDir, map[string]string{
			".claude-plugin/marketplace.json": mpJSON,
			"README.md":                       "# Test Marketplace",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"my-alias": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Marketplace should be cloned with real name (from marketplace.json), not alias
		realNameDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "jlaswell-community-marketplace")
		if _, err := os.Stat(realNameDir); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned under real name 'jlaswell-community-marketplace'")
		}

		// Alias directory should NOT exist
		aliasDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "my-alias")
		if _, err := os.Stat(aliasDir); err == nil {
			t.Error("Expected alias 'my-alias' NOT to be used as directory name")
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("skips marketplace when marketplace.json is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo WITHOUT .claude-plugin/marketplace.json
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test Marketplace without marketplace.json",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"broken-marketplace": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		// Should not fail, but marketplace should be skipped (no marketplace.json)
		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// No marketplace directory should be created (was skipped due to missing marketplace.json)
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		entries, _ := os.ReadDir(marketplacesDir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".tmp-") {
				t.Errorf("Unexpected marketplace directory: %s (marketplace should have been skipped)", e.Name())
			}
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("multiple marketplaces cloned with real names", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create first marketplace
		sourceDir1 := filepath.Join(tmpDir, "marketplace1")
		createTestGitRepo(t, sourceDir1, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "first-real-name"}`,
		})

		// Create second marketplace
		sourceDir2 := filepath.Join(tmpDir, "marketplace2")
		createTestGitRepo(t, sourceDir2, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "second-real-name"}`,
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"alias1": {URL: sourceDir1},
				"alias2": {URL: sourceDir2},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")

		// Both marketplaces should be cloned with real names
		if _, err := os.Stat(filepath.Join(marketplacesDir, "first-real-name")); os.IsNotExist(err) {
			t.Error("Expected 'first-real-name' marketplace directory to exist")
		}
		if _, err := os.Stat(filepath.Join(marketplacesDir, "second-real-name")); os.IsNotExist(err) {
			t.Error("Expected 'second-real-name' marketplace directory to exist")
		}

		// Alias directories should NOT exist
		if _, err := os.Stat(filepath.Join(marketplacesDir, "alias1")); err == nil {
			t.Error("Expected alias 'alias1' NOT to be used as directory name")
		}
		if _, err := os.Stat(filepath.Join(marketplacesDir, "alias2")); err == nil {
			t.Error("Expected alias 'alias2' NOT to be used as directory name")
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

}

func TestGetGHESHost(t *testing.T) {
	save := func(key string) func() {
		orig, ok := os.LookupEnv(key)
		return func() {
			if ok {
				_ = os.Setenv(key, orig)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}

	tests := []struct {
		name     string
		apiEnv   string
		expected string
	}{
		{
			name:     "unset",
			apiEnv:   "",
			expected: "",
		},
		{
			name:     "github.com API",
			apiEnv:   "https://api.github.com",
			expected: "",
		},
		{
			name:     "GHES URL with /api/v3 suffix",
			apiEnv:   "https://github.enterprise.com/api/v3",
			expected: "github.enterprise.com",
		},
		{
			name:     "GHES URL without suffix",
			apiEnv:   "https://github.enterprise.com",
			expected: "github.enterprise.com",
		},
		{
			name:     "GHES URL with http",
			apiEnv:   "http://github.internal.com/api/v3",
			expected: "github.internal.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := save("GITHUB_API")
			defer restore()
			if tt.apiEnv == "" {
				_ = os.Unsetenv("GITHUB_API")
			} else {
				_ = os.Setenv("GITHUB_API", tt.apiEnv)
			}
			got := getGHESHost()
			if got != tt.expected {
				t.Errorf("getGHESHost() = %q, want %q", got, tt.expected)
			}
		})
	}
}
