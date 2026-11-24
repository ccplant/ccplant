package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncMarketplaces_Case01(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	opts := SyncOptions{
		OutputDir: outputDir,
	}

	err := syncMarketplaces(opts, nil)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// Verify directories were created
	if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
		t.Error("Expected .claude directory to be created")
	}
	marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
	if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
		t.Error("Expected marketplaces directory to be created")
	}

	// settings.json should NOT be created by syncMarketplaces (compile's job)
	settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
	}
}

func TestSyncMarketplaces_Case02(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	settings := &settingsJSON{
		Name: "test-user",
		Marketplaces: map[string]*marketplaceJSON{
			"empty-url": {URL: ""},
		},
	}

	opts := SyncOptions{OutputDir: outputDir}

	err := syncMarketplaces(opts, settings)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// No marketplace should be cloned
	marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
	entries, _ := os.ReadDir(marketplacesDir)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("Unexpected marketplace directory: %s", e.Name())
		}
	}
}

func TestSyncMarketplaces_Case03(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	settings := &settingsJSON{
		Name:         "test-user",
		Marketplaces: map[string]*marketplaceJSON{},
	}

	opts := SyncOptions{OutputDir: outputDir}

	err := syncMarketplaces(opts, settings)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// Directories should be created even with no marketplaces
	if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
		t.Error("Expected .claude directory to be created")
	}
}

func TestSyncMarketplaces_Case04(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	sourceDir := filepath.Join(tmpDir, "marketplace-source")
	createTestGitRepo(t, sourceDir, map[string]string{
		"README.md":                       "# Test Marketplace",
		".claude-plugin/marketplace.json": `{"name": "no-plugins"}`,
	})

	settings := &settingsJSON{
		Name: "test-user",
		Marketplaces: map[string]*marketplaceJSON{
			"no-plugins": {URL: sourceDir},
		},
		EnabledPlugins: nil,
	}

	opts := SyncOptions{OutputDir: outputDir}

	err := syncMarketplaces(opts, settings)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// Marketplace should be cloned with real name
	clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "no-plugins")
	if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
		t.Error("Expected marketplace to be cloned as 'no-plugins'")
	}
}

func TestSyncMarketplaces_Case05(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	validSourceDir := filepath.Join(tmpDir, "valid-marketplace")
	createTestGitRepo(t, validSourceDir, map[string]string{
		"README.md":                       "# Valid Marketplace",
		".claude-plugin/marketplace.json": `{"name": "valid"}`,
	})

	settings := &settingsJSON{
		Name: "test-user",
		Marketplaces: map[string]*marketplaceJSON{
			"valid":     {URL: validSourceDir},
			"invalid":   {URL: "https://invalid.example.com/nonexistent.git"},
			"empty-url": {URL: ""},
		},
	}

	opts := SyncOptions{OutputDir: outputDir}

	// Should not fail even though some marketplaces fail
	err := syncMarketplaces(opts, settings)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// Only valid marketplace should be cloned
	clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "valid")
	if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
		t.Error("Expected 'valid' marketplace to be cloned")
	}
}

func TestSyncMarketplaces_Case06(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "output")

	sourceDir := filepath.Join(tmpDir, "marketplace")
	createTestGitRepo(t, sourceDir, map[string]string{
		"plugin.json":                     `{"name": "test"}`,
		".claude-plugin/marketplace.json": `{"name": "my-marketplace"}`,
	})

	settings := &settingsJSON{
		Name: "test-user",
		Marketplaces: map[string]*marketplaceJSON{
			"alias-key": {URL: sourceDir},
		},
	}

	opts := SyncOptions{OutputDir: outputDir}

	err := syncMarketplaces(opts, settings)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	// Cloned with real name (from marketplace.json), not alias
	expectedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "my-marketplace")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Error("Expected marketplace cloned as real name 'my-marketplace'")
	}
}

func TestSyncMarketplaces_Case07(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "a", "b", "c", "output")

	opts := SyncOptions{OutputDir: outputDir}

	err := syncMarketplaces(opts, nil)
	if err != nil {
		t.Fatalf("syncMarketplaces failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
		t.Error("Expected .claude directory to be created")
	}
	marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
	if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
		t.Error("Expected marketplaces directory to be created")
	}
}
