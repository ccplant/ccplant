package sessionsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CodexModelOptionsEnv is the session environment variable that carries the
	// model ids the ACP chat model switcher should be able to select, as a JSON
	// array of strings.
	CodexModelOptionsEnv = "CODEX_MODEL_OPTIONS"
	// CodexModelCatalogFile is the catalog written next to config.toml.
	CodexModelCatalogFile = "ccplant-model-catalog.json"
	// codexModelCatalogTimeout bounds the bundled-catalog read so a broken Codex
	// installation cannot stall session provisioning.
	codexModelCatalogTimeout = 30 * time.Second
)

// codexModelCatalog keeps entries as raw maps so unknown fields survive the
// round trip. Codex validates the catalog strictly and version-specific fields
// must not be dropped.
type codexModelCatalog struct {
	Models []map[string]any `json:"models"`
}

// generateCodexModelCatalog registers the session profile's model candidates in
// the Codex model catalog. Codex replaces its built-in catalog with the file
// referenced by `model_catalog_json`, therefore entries are cloned from the
// bundled catalog of the installed Codex binary to stay schema compatible.
//
// Returns the catalog path, or "" when no catalog should be written (no
// candidates configured, or the bundled catalog is unavailable).
func generateCodexModelCatalog(outputDir string, env map[string]string) string {
	candidates := codexModelOptionIDs(env)
	if len(candidates) == 0 {
		return ""
	}

	catalog, err := codexBundledModelCatalog(env)
	if err != nil {
		log.Printf("[COMPILE-SETTINGS] Skipping Codex model catalog: %v", err)
		return ""
	}

	current := strings.TrimSpace(env["CODEX_MODEL"])
	if current == "" {
		current = strings.TrimSpace(env["OPENAI_MODEL"])
	}
	// The session model itself must stay selectable even when it is not one of
	// the profile candidates.
	wanted := append([]string{}, candidates...)
	if current != "" {
		wanted = append(wanted, current)
	}

	known := map[string]bool{}
	for _, model := range catalog.Models {
		if slug, ok := model["slug"].(string); ok {
			known[slug] = true
		}
	}
	reference := codexModelCatalogReference(catalog.Models, current)
	if reference == nil {
		log.Printf("[COMPILE-SETTINGS] Skipping Codex model catalog: bundled catalog has no usable model entry")
		return ""
	}

	added := 0
	for _, slug := range wanted {
		if slug == "" || known[slug] {
			continue
		}
		known[slug] = true
		entry := codexModelCatalogEntry(reference, slug)
		catalog.Models = append(catalog.Models, entry)
		added++
	}
	if added == 0 {
		return ""
	}

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		log.Printf("[COMPILE-SETTINGS] Skipping Codex model catalog: %v", err)
		return ""
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		log.Printf("[COMPILE-SETTINGS] Skipping Codex model catalog: %v", err)
		return ""
	}
	catalogPath := filepath.Join(codexDir, CodexModelCatalogFile)
	if err := os.WriteFile(catalogPath, encoded, 0644); err != nil {
		log.Printf("[COMPILE-SETTINGS] Skipping Codex model catalog: %v", err)
		return ""
	}
	log.Printf("[COMPILE-SETTINGS] Registered %d model candidate(s) in %s", added, catalogPath)
	return catalogPath
}

// codexModelCatalogEnv narrows the session environment to the keys the catalog
// generator needs. A resolved Codex connection owns the session model, so its
// value is preferred over the profile environment.
func codexModelCatalogEnv(settings *SessionSettings) map[string]string {
	env := map[string]string{}
	for _, key := range []string{"CODEX_PATH", CodexModelOptionsEnv, "CODEX_MODEL", "OPENAI_MODEL"} {
		if value := strings.TrimSpace(settings.Env[key]); value != "" {
			env[key] = value
		}
	}
	if settings.CodexConnection != nil {
		if model := strings.TrimSpace(settings.CodexConnection.Model); model != "" {
			env["CODEX_MODEL"] = model
		}
	}
	return env
}

// codexModelOptionIDs parses CODEX_MODEL_OPTIONS. A JSON array is the canonical
// form; comma or newline separated values are accepted as a fallback.
func codexModelOptionIDs(env map[string]string) []string {
	raw := strings.TrimSpace(env[CodexModelOptionsEnv])
	if raw == "" {
		return nil
	}

	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		values = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		})
	}

	seen := map[string]bool{}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// codexBundledModelCatalog reads the catalog shipped with the installed Codex
// binary. --bundled skips the remote refresh and ignores model_catalog_json, so
// this always returns Codex's own entries.
func codexBundledModelCatalog(env map[string]string) (*codexModelCatalog, error) {
	codexPath := strings.TrimSpace(env["CODEX_PATH"])
	if codexPath == "" {
		codexPath = "codex"
	}

	ctx, cancel := context.WithTimeout(context.Background(), codexModelCatalogTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, codexPath, "debug", "models", "--bundled").Output()
	if err != nil {
		return nil, fmt.Errorf("codex debug models --bundled failed: %w", err)
	}

	var catalog codexModelCatalog
	if err := json.Unmarshal(output, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse the bundled Codex model catalog: %w", err)
	}
	if len(catalog.Models) == 0 {
		return nil, errors.New("bundled Codex model catalog is empty")
	}
	return &catalog, nil
}

// codexModelCatalogReference picks the entry cloned for custom models. The
// current model is preferred because its prompt scaffolding and tool settings
// best match what the session already runs.
func codexModelCatalogReference(models []map[string]any, current string) map[string]any {
	if current != "" {
		for _, model := range models {
			if slug, ok := model["slug"].(string); ok && slug == current {
				return model
			}
		}
	}
	for _, model := range models {
		if visibility, ok := model["visibility"].(string); ok && visibility == "list" {
			return model
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return nil
}

// codexModelCatalogEntry clones the reference entry and renames it. The clone
// keeps Codex's own instructions and tool configuration, which the catalog
// schema requires for every model.
func codexModelCatalogEntry(reference map[string]any, slug string) map[string]any {
	entry := make(map[string]any, len(reference)+1)
	for key, value := range reference {
		entry[key] = value
	}
	entry["slug"] = slug
	entry["display_name"] = slug
	entry["description"] = fmt.Sprintf("Custom model registered from the session profile (%s)", slug)
	// The reference entry may be hidden from the picker; candidates must not be.
	entry["visibility"] = "list"
	// Newer bundled models opt into "responses lite": tools travel as
	// additional_tools input items, which OpenAI-compatible providers behind a
	// custom base_url generally reject. Custom models use the classic format.
	entry["use_responses_lite"] = false
	delete(entry, "tool_mode")
	entry["experimental_supported_tools"] = []any{}
	return entry
}

// appendCodexModelCatalogConfig pins config.toml to the generated catalog.
func appendCodexModelCatalogConfig(content string, catalogPath string) string {
	line := "model_catalog_json = " + tomlString(catalogPath)
	content = removeTopLevelTOMLKey(content, "model_catalog_json")
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return line + "\n"
	}
	return line + "\n" + content + "\n"
}
