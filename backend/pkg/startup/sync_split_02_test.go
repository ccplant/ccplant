package startup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTestSettingsJSON writes a minimal settings.json to simulate what compile generates.
func writeTestSettingsJSON(t *testing.T, outputDir string, content map[string]interface{}) {
	t.Helper()
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write settings.json: %v", err)
	}
}

func TestGenerateClaudeJSON_Case01(t *testing.T) {
	tmpDir := t.TempDir()

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	// Verify file was created
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	if result["hasCompletedOnboarding"] != true {
		t.Error("Expected hasCompletedOnboarding to be true")
	}
	if result["bypassPermissionsModeAccepted"] != true {
		t.Error("Expected bypassPermissionsModeAccepted to be true")
	}
}

func TestGenerateClaudeJSON_Case02(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

	// Create existing file with extra fields
	existingData := map[string]interface{}{
		"existingField":     "existingValue",
		"anotherField":      123,
		"shouldBePreserved": true,
	}
	data, _ := json.Marshal(existingData)
	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	// Verify existing fields are preserved
	data, err = os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	if result["existingField"] != "existingValue" {
		t.Error("Expected existingField to be preserved")
	}
	if result["anotherField"] != float64(123) { // JSON numbers are float64
		t.Error("Expected anotherField to be preserved")
	}
	if result["shouldBePreserved"] != true {
		t.Error("Expected shouldBePreserved to be preserved")
	}
	if result["hasCompletedOnboarding"] != true {
		t.Error("Expected hasCompletedOnboarding to be true")
	}
	if result["bypassPermissionsModeAccepted"] != true {
		t.Error("Expected bypassPermissionsModeAccepted to be true")
	}
}

func TestGenerateClaudeJSON_Case03(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

	// Create existing file with invalid JSON
	if err := os.WriteFile(claudeJSONPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	// Verify file was overwritten with valid JSON
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	if result["hasCompletedOnboarding"] != true {
		t.Error("Expected hasCompletedOnboarding to be true")
	}
}

func TestGenerateClaudeJSON_Case04(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

	// Create existing file with onboarding fields set to false
	existingData := map[string]interface{}{
		"hasCompletedOnboarding":        false,
		"bypassPermissionsModeAccepted": false,
		"someOtherField":                "value",
	}
	data, _ := json.Marshal(existingData)
	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	data, err = os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	// These should be overwritten to true
	if result["hasCompletedOnboarding"] != true {
		t.Error("Expected hasCompletedOnboarding to be overwritten to true")
	}
	if result["bypassPermissionsModeAccepted"] != true {
		t.Error("Expected bypassPermissionsModeAccepted to be overwritten to true")
	}
	// Other field should be preserved
	if result["someOtherField"] != "value" {
		t.Error("Expected someOtherField to be preserved")
	}
}

func TestGenerateClaudeJSON_Case05(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

	existingData := map[string]interface{}{
		"nested": map[string]interface{}{
			"level1": map[string]interface{}{
				"level2": "deep value",
			},
		},
		"array": []interface{}{"item1", "item2", "item3"},
	}
	data, _ := json.Marshal(existingData)
	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	data, err = os.ReadFile(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	// Verify nested structure is preserved
	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected nested to be preserved")
	}
	level1, ok := nested["level1"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected level1 to be preserved")
	}
	if level1["level2"] != "deep value" {
		t.Error("Expected deep nested value to be preserved")
	}

	// Verify array is preserved
	arr, ok := result["array"].([]interface{})
	if !ok {
		t.Fatal("Expected array to be preserved")
	}
	if len(arr) != 3 {
		t.Errorf("Expected 3 array items, got %d", len(arr))
	}
}

func TestGenerateClaudeJSON_Case06(t *testing.T) {
	tmpDir := t.TempDir()

	err := generateClaudeJSON(tmpDir)
	if err != nil {
		t.Fatalf("generateClaudeJSON failed: %v", err)
	}

	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	info, err := os.Stat(claudeJSONPath)
	if err != nil {
		t.Fatalf("Failed to stat .claude.json: %v", err)
	}

	// Check that file is readable and writable by owner (0644)
	perm := info.Mode().Perm()
	if perm&0600 != 0600 {
		t.Errorf("Expected file to be readable and writable by owner, got %o", perm)
	}
}
