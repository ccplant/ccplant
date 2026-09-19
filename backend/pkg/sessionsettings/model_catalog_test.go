package sessionsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

// stubBundledCatalog mimics `codex debug models --bundled`. The hidden entry is
// used to verify the reference selection prefers a listed model.
const stubBundledCatalog = `{
  "models": [
    {"slug": "gpt-5.6-sol", "display_name": "GPT 5.6 Sol", "description": "sol", "visibility": "hidden", "base_instructions": "sol instructions", "priority": 1},
    {"slug": "gpt-5.6-terra", "display_name": "GPT 5.6 Terra", "description": "terra", "visibility": "list", "base_instructions": "terra instructions", "priority": 2, "use_responses_lite": true, "tool_mode": "code_mode_only", "experimental_supported_tools": ["clock"]}
  ]
}`

func installStubCodex(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\ncat <<'CATALOG_EOF'\n" + output + "\nCATALOG_EOF\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

func readGeneratedCatalog(t *testing.T, outputDir string) []map[string]any {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(outputDir, ".codex", CodexModelCatalogFile))
	require.NoError(t, err)
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(encoded, &catalog))
	return catalog.Models
}

func catalogSlugs(models []map[string]any) []string {
	slugs := make([]string, 0, len(models))
	for _, model := range models {
		slug, _ := model["slug"].(string)
		slugs = append(slugs, slug)
	}
	return slugs
}

func findCatalogEntry(t *testing.T, models []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, model := range models {
		if model["slug"] == slug {
			return model
		}
	}
	t.Fatalf("model %q not found in catalog", slug)
	return nil
}

func TestGenerateCodexModelCatalogWithoutOptions(t *testing.T) {
	outputDir := t.TempDir()
	require.Empty(t, generateCodexModelCatalog(outputDir, map[string]string{"CODEX_PATH": installStubCodex(t, stubBundledCatalog)}))
	_, err := os.Stat(filepath.Join(outputDir, ".codex", CodexModelCatalogFile))
	require.True(t, os.IsNotExist(err))
}

func TestGenerateCodexConfigTOMLWithoutOptions(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, generateCodexConfigTOML(outputDir, "", map[string]string{}, nil))
	_, err := os.Stat(filepath.Join(outputDir, ".codex", "config.toml"))
	require.True(t, os.IsNotExist(err))
}

func TestGenerateCodexModelCatalogAddsCandidates(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		"CODEX_MODEL":        "gpt-5.6-sol",
		CodexModelOptionsEnv: `["glm-5.2", "glm-5.3", "glm-5.2", "", "  "]`,
	}
	catalogPath := generateCodexModelCatalog(outputDir, env)
	require.Equal(t, filepath.Join(outputDir, ".codex", CodexModelCatalogFile), catalogPath)

	models := readGeneratedCatalog(t, outputDir)
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-terra", "glm-5.2", "glm-5.3"}, catalogSlugs(models))

	// The bundled entry for the current model is cloned, so its instructions and
	// tool settings carry over, and it stays visible in the picker.
	custom := findCatalogEntry(t, models, "glm-5.3")
	require.Equal(t, "glm-5.3", custom["display_name"])
	require.Equal(t, "list", custom["visibility"])
	require.Equal(t, "sol instructions", custom["base_instructions"])
	require.Contains(t, custom["description"], "glm-5.3")
}

func TestGenerateCodexModelCatalogKeepsBundledEntries(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		CodexModelOptionsEnv: `["glm-5.3"]`,
	}
	require.NotEmpty(t, generateCodexModelCatalog(outputDir, env))

	models := readGeneratedCatalog(t, outputDir)
	terra := findCatalogEntry(t, models, "gpt-5.6-terra")
	require.Equal(t, "GPT 5.6 Terra", terra["display_name"])
	require.Equal(t, "terra", terra["description"])
	// Bundled entries stay untouched, including their wire format opt-ins.
	require.Equal(t, true, terra["use_responses_lite"])
	require.Equal(t, "code_mode_only", terra["tool_mode"])
}

func TestGenerateCodexModelCatalogIncludesCurrentModel(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		"CODEX_MODEL":        "custom-current",
		CodexModelOptionsEnv: `["glm-5.3"]`,
	}
	require.NotEmpty(t, generateCodexModelCatalog(outputDir, env))

	models := readGeneratedCatalog(t, outputDir)
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-terra", "glm-5.3", "custom-current"}, catalogSlugs(models))
}

func TestGenerateCodexModelCatalogUsesListedReference(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		CodexModelOptionsEnv: `["glm-5.3"]`,
	}
	require.NotEmpty(t, generateCodexModelCatalog(outputDir, env))

	custom := findCatalogEntry(t, readGeneratedCatalog(t, outputDir), "glm-5.3")
	require.Equal(t, "terra instructions", custom["base_instructions"])
	// Custom models must not inherit "responses lite": providers behind a
	// custom base_url reject the additional_tools input items it produces.
	require.Equal(t, false, custom["use_responses_lite"])
	require.NotContains(t, custom, "tool_mode")
	require.Equal(t, []any{}, custom["experimental_supported_tools"])
}

func TestGenerateCodexModelCatalogSkipsKnownCandidates(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		"CODEX_MODEL":        "gpt-5.6-sol",
		CodexModelOptionsEnv: `["gpt-5.6-terra"]`,
	}
	// Every candidate already exists in the bundled catalog, so replacing the
	// catalog would have no effect.
	require.Empty(t, generateCodexModelCatalog(outputDir, env))
	_, err := os.Stat(filepath.Join(outputDir, ".codex", CodexModelCatalogFile))
	require.True(t, os.IsNotExist(err))
}

func TestGenerateCodexModelCatalogBundledCatalogFailures(t *testing.T) {
	t.Run("missing codex binary", func(t *testing.T) {
		outputDir := t.TempDir()
		env := map[string]string{
			"CODEX_PATH":         filepath.Join(t.TempDir(), "missing-codex"),
			CodexModelOptionsEnv: `["glm-5.3"]`,
		}
		require.Empty(t, generateCodexModelCatalog(outputDir, env))
	})
	t.Run("invalid json", func(t *testing.T) {
		outputDir := t.TempDir()
		env := map[string]string{
			"CODEX_PATH":         installStubCodex(t, "not json"),
			CodexModelOptionsEnv: `["glm-5.3"]`,
		}
		require.Empty(t, generateCodexModelCatalog(outputDir, env))
	})
	t.Run("empty models", func(t *testing.T) {
		outputDir := t.TempDir()
		env := map[string]string{
			"CODEX_PATH":         installStubCodex(t, `{"models": []}`),
			CodexModelOptionsEnv: `["glm-5.3"]`,
		}
		require.Empty(t, generateCodexModelCatalog(outputDir, env))
	})
}

func TestGenerateCodexConfigTOMLRegistersCatalog(t *testing.T) {
	outputDir := t.TempDir()
	env := map[string]string{
		"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
		"CODEX_MODEL":        "glm-5.3",
		CodexModelOptionsEnv: `["glm-5.3"]`,
		"OPENAI_BASE_URL":    "https://ollama.example/v1",
	}
	require.NoError(t, generateCodexConfigTOML(outputDir, "approval-mode = \"full-auto\"\n", env, env))

	encoded, err := os.ReadFile(filepath.Join(outputDir, ".codex", "config.toml"))
	require.NoError(t, err)
	content := string(encoded)
	require.Contains(t, content, "model_catalog_json")
	require.Contains(t, content, CodexModelCatalogFile)
	require.Contains(t, content, "approval-mode")
	require.Contains(t, content, "agentapi_openai_compatible")
}

func TestCodexModelOptionIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "json array", raw: `["glm-5.2","glm-5.3"]`, want: []string{"glm-5.2", "glm-5.3"}},
		{name: "deduplicates", raw: `["a","b","a"]`, want: []string{"a", "b"}},
		{name: "trims and drops blanks", raw: `[" a ", "", "b"]`, want: []string{"a", "b"}},
		{name: "comma fallback", raw: "a, b\na", want: []string{"a", "b"}},
		{name: "empty", raw: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, codexModelOptionIDs(map[string]string{CodexModelOptionsEnv: tt.raw}))
		})
	}
}

func TestAppendCodexModelCatalogConfig(t *testing.T) {
	t.Run("empty content", func(t *testing.T) {
		require.Equal(t, "model_catalog_json = \"/tmp/catalog.json\"\n", appendCodexModelCatalogConfig("", "/tmp/catalog.json"))
	})
	t.Run("replaces existing key", func(t *testing.T) {
		content := "model_catalog_json = \"/old.json\"\napproval-mode = \"full-auto\"\n"
		result := appendCodexModelCatalogConfig(content, "/new catalog.json")
		require.Equal(t, 1, strings.Count(result, "model_catalog_json"))
		require.Contains(t, result, "\"/new catalog.json\"")
		require.Contains(t, result, "approval-mode = \"full-auto\"")
	})
	t.Run("does not touch nested keys", func(t *testing.T) {
		content := "[model_providers.x]\nmodel_catalog_json = \"/nested.json\"\n"
		result := appendCodexModelCatalogConfig(content, "/new.json")
		require.Contains(t, result, "\"/nested.json\"")
	})
}

func TestCompileSettingsRegistersCatalogWithConnection(t *testing.T) {
	outputDir := t.TempDir()
	settings := &SessionSettings{
		Session: SessionMeta{ID: "session", UserID: "user", Scope: "user", AgentType: "codex-acp"},
		Env: map[string]string{
			"CODEX_PATH":         installStubCodex(t, stubBundledCatalog),
			CodexModelOptionsEnv: `["glm-5.3"]`,
		},
		// An explicit compatible connection owns the provider TOML, but must not
		// suppress the model catalog.
		CodexConnection: &modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://ollama.example/v1", Model: "glm-5.3", Authentication: "none"},
	}
	require.NoError(t, CompileSettings(settings, CompileOptions{OutputDir: outputDir, StartupPath: filepath.Join(outputDir, "startup.sh")}))

	require.FileExists(t, filepath.Join(outputDir, ".codex", CodexModelCatalogFile))
	encoded, err := os.ReadFile(filepath.Join(outputDir, ".codex", "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(encoded), "model_catalog_json")
	require.Contains(t, string(encoded), CodexModelCatalogFile)
	require.Contains(t, string(encoded), "model = 'glm-5.3'")

	models := readGeneratedCatalog(t, outputDir)
	require.Contains(t, catalogSlugs(models), "glm-5.3")
}
