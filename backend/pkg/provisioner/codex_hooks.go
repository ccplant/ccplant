package provisioner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/pelletier/go-toml/v2"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func prepareCodexHooks(outputDir string, injected map[string]interface{}) error {
	managed := sessionsettings.MergeCodexManagedHooks(injected)
	// Preserve organisation requirements and their policy restrictions. Never
	// replace a pre-existing requirements file with just the session hooks.
	requirements := map[string]interface{}{}
	data, err := os.ReadFile(codexRequirementsPath)
	if err == nil {
		if err := toml.Unmarshal(data, &requirements); err != nil {
			return fmt.Errorf("parse Codex managed requirements: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	original := sessionsettings.MergeCodexHooks(requirements, nil)
	merged := sessionsettings.MergeCodexHooks(requirements, managed)
	hooks := merged["hooks"].(map[string]interface{})
	// Notification is Claude-specific. Keep all other event metadata intact.
	delete(hooks, "Notification")
	if len(hooks) > 0 {
		if _, ok := hooks["managed_dir"]; !ok && requirements["hooks"] == nil {
			hooks["managed_dir"] = filepath.Dir(codexRequirementsPath)
		}
		requirements["hooks"] = hooks
		// An organisation may mount this file read-only. If it already supplies
		// the required hooks, keep it intact and remove only the user duplicates.
		if !reflect.DeepEqual(original["hooks"], hooks) {
			data, err := toml.Marshal(requirements)
			if err != nil {
				return err
			}
			if err := writeCodexRequirementsToPath(codexRequirementsPath, string(data)); err != nil {
				return err
			}
		}
	}
	// This runs after snapshot and managed-file restoration, so old duplicate
	// hooks cannot reappear when a Pod is replaced or a session is resumed.
	path := filepath.Join(outputDir, ".codex", "hooks.json")
	data, err = os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var user map[string]interface{}
	if err := json.Unmarshal(data, &user); err != nil {
		return fmt.Errorf("parse Codex user hooks: %w", err)
	}
	data, err = json.MarshalIndent(sessionsettings.RemoveManagedCodexHooks(user, requirements), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
