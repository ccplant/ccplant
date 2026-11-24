package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteAntigravityMCPConfigNormalizesRemoteServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gemini", "config", "mcp_config.json")
	servers := map[string]interface{}{
		"remote": map[string]interface{}{"type": "http", "url": "https://mcp.example", "headers": map[string]interface{}{"X-Test": "yes"}},
		"stdio":  map[string]interface{}{"type": "stdio", "command": "demo-mcp", "args": []interface{}{"serve"}},
	}
	if err := writeAntigravityMCPConfig(path, servers); err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &got) != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	configured := got["mcpServers"].(map[string]interface{})
	remote := configured["remote"].(map[string]interface{})
	if remote["serverUrl"] != "https://mcp.example" || remote["url"] != nil || remote["type"] != nil {
		t.Fatalf("remote MCP config = %#v", remote)
	}
	stdio := configured["stdio"].(map[string]interface{})
	if stdio["command"] != "demo-mcp" || stdio["type"] != nil {
		t.Fatalf("stdio MCP config = %#v", stdio)
	}
}

func TestSyncedManagedFilePathsExcludesUnsyncedPaths(t *testing.T) {
	got := syncedManagedFilePaths([]string{
		" /home/agentapi/.codex/auth.json ",
		"",
	})
	want := []string{
		"/home/agentapi/.claude/.credentials.json",
		"/home/agentapi/.ccplant/antigravity-auth.json",
		"/home/agentapi/.config/gcloud/application_default_credentials.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("syncedManagedFilePaths() = %#v, want %#v", got, want)
	}
}

// TestWriteWebhookPayloadFile_SkipsWhenPresent verifies that
// writeWebhookPayloadFile does NOT overwrite an existing file (non-stock case:
// Secret volume mount already provides the file as read-only).
func TestWriteWebhookPayloadFile_SkipsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	existing := `{"original":"content"}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	writeWebhookPayloadFileToPath(path, `{"should":"not-overwrite"}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(got) != existing {
		t.Errorf("expected original content %q to be preserved, got %q", existing, string(got))
	}
}

// TestWriteWebhookPayloadFile_CreatesParentDirs verifies that parent
// directories are created as needed.
func TestWriteWebhookPayloadFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "payload.json")

	writeWebhookPayloadFileToPath(path, `{"nested":true}`)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}

// TestMergeHooksIntoSettingsFile_ManagedOverwritesCompiled verifies that when
// a managed settings.json has overwritten the compiled one, mergeHooksIntoSettingsFile
// restores the compiled hooks without losing the managed file's other settings.
func TestMergeHooksIntoSettingsFile_ManagedOverwritesCompiled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Simulate: managed settings.json (no hooks — user's saved version)
	managed := map[string]interface{}{
		"autoUpdatesChannel": "stable",
		"theme":              "dark",
	}
	managedData, _ := json.MarshalIndent(managed, "", "  ")
	if err := os.WriteFile(path, managedData, 0644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	// Compiled settings with Stop hook for oneshot deletion
	compiledSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "ccplant client delete-session --confirm",
						},
					},
				},
			},
		},
	}

	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("mergeHooksIntoSettingsFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// User settings must be preserved
	if result["theme"] != "dark" {
		t.Errorf("expected theme=dark to be preserved, got %v", result["theme"])
	}

	// Compiled hooks must be present
	hooks, ok := result["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks to be map, got %T", result["hooks"])
	}
	stopHooks, ok := hooks["Stop"]
	if !ok {
		t.Fatal("expected Stop hook to be present after merge")
	}
	stopList, ok := stopHooks.([]interface{})
	if !ok || len(stopList) == 0 {
		t.Fatalf("expected Stop hooks list, got %T: %v", stopHooks, stopHooks)
	}
}

// TestMergeHooksIntoSettingsFile_PreservesExistingHooks verifies that hooks
// already in the managed settings.json are preserved when no compiled hook
// overrides them.
func TestMergeHooksIntoSettingsFile_PreservesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Managed file has a Notification hook
	managed := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Notification": []interface{}{
				map[string]interface{}{"hooks": []interface{}{
					map[string]interface{}{"type": "command", "command": "echo notif"},
				}},
			},
		},
	}
	managedData, _ := json.MarshalIndent(managed, "", "  ")
	if err := os.WriteFile(path, managedData, 0644); err != nil {
		t.Fatalf("write managed file: %v", err)
	}

	// Compiled settings only has a Stop hook
	compiledSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "ccplant client delete-session --confirm"},
					},
				},
			},
		},
	}

	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("mergeHooksIntoSettingsFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)

	hooks := result["hooks"].(map[string]interface{})
	if _, ok := hooks["Notification"]; !ok {
		t.Error("expected Notification hook to be preserved from managed file")
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Error("expected Stop hook from compiled settings to be added")
	}
}

// TestMergeHooksIntoSettingsFile_NoHooksInCompiled verifies that when
// compiledSettings has no hooks, the function is a no-op.
func TestMergeHooksIntoSettingsFile_NoHooksInCompiled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	original := `{"theme":"light"}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	compiledSettings := map[string]interface{}{"autoUpdatesChannel": "stable"}
	if err := mergeHooksIntoSettingsFile(path, compiledSettings); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should be unchanged
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("expected file unchanged, got %q", string(got))
	}
}

func TestBuildCodexRequirementsTOML(t *testing.T) {
	hooksJSON := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "ccplant client memory save-session >> /tmp/memory-save-session.log 2>&1",
						},
					},
				},
			},
			"Notification": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "ccplant client send-notification",
						},
					},
				},
			},
		},
	}

	got, err := buildCodexRequirementsTOML(hooksJSON)
	if err != nil {
		t.Fatalf("buildCodexRequirementsTOML: %v", err)
	}

	mustContain := []string{
		"[hooks]",
		"managed_dir = \"/etc/codex\"",
		"[[hooks.Stop]]",
		"[[hooks.Stop.hooks]]",
		"type = \"command\"",
		"command = \"ccplant client memory save-session >> /tmp/memory-save-session.log 2>&1\"",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("expected TOML to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Notification") {
		t.Fatalf("Notification hooks should not be written for Codex, got:\n%s", got)
	}
}

func TestWriteCodexRequirementsToPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "codex", "requirements.toml")
	content := "[hooks]\nmanaged_dir = \"/etc/codex\"\n"

	if err := writeCodexRequirementsToPath(path, content); err != nil {
		t.Fatalf("writeCodexRequirementsToPath: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read requirements: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}
