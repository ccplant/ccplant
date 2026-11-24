package sessionsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func TestCompile_CodexConfigTOML_PassesAManagedProviderDirectlyToCodexAcp(t *testing.T) {
	tmpDir := t.TempDir()
	settings := &SessionSettings{
		Session: SessionMeta{AgentType: "codex-acp"},
		Env:     map[string]string{"CCPLANT_CODEX_API_KEY": "gateway-key"},
		Codex:   CodexConfig{ConfigTOML: "sandbox_mode = \"danger-full-access\"\n"},
		CodexConnection: &modelprovider.Connection{
			Mode: "openai_compatible", BaseURL: "https://gateway.example/v1",
			Model: "profile-model", Authentication: "api_key",
		},
	}

	err := CompileSettings(settings, CompileOptions{
		OutputDir:   tmpDir,
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	})
	require.NoError(t, err)
	require.Equal(t, codexCustomOpenAIProviderID, settings.Env["MODEL_PROVIDER"])

	var config map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(settings.Env["CODEX_CONFIG"]), &config))
	require.Equal(t, "profile-model", config["model"])
	require.Equal(t, codexCustomOpenAIProviderID, config["model_provider"])
	providers := config["model_providers"].(map[string]interface{})
	provider := providers[codexCustomOpenAIProviderID].(map[string]interface{})
	require.Equal(t, "https://gateway.example/v1", provider["base_url"])
	require.Equal(t, "OPENAI_API_KEY", provider["env_key"])
	require.Equal(t, false, provider["requires_openai_auth"])
	// The generated catalog path belongs in the on-disk config.toml only.
	// CODEX_CONFIG is consumed by codex-acp in the agent container and must
	// not carry a provisioner-side absolute path.
	require.NotContains(t, config, "model_catalog_json")
}

func TestCompile_CodexConfigTOML_WritesConfigTomlWhenCodexConfigTOMLIsSet(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex",
			UserID:    "user-codex",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Codex: CodexConfig{
			ConfigTOML: "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	assert.FileExists(t, configPath)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n", string(data))
}

func TestCompile_CodexConfigTOML_SkipsConfigTomlWhenCodexConfigTOMLIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-no-codex",
			UserID: "user-no-codex",
			Scope:  "user",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(outputDir, ".codex/config.toml"))
}

func TestCompile_CodexConfigTOML_WritesCustomProviderWhenOPENAIBASEURLIsSet(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-custom-provider",
			UserID:    "user-codex-custom-provider",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Env: map[string]string{
			"OPENAI_BASE_URL": "http://ollama:11434/v1",
			"OPENAI_API_KEY":  "test-api-key",
			"OPENAI_MODEL":    "qwen3-coder-next",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `model_provider = "agentapi_openai_compatible"`)
	assert.Contains(t, content, `model = "qwen3-coder-next"`)
	assert.Contains(t, content, `model_context_window = 128000`)
	assert.Contains(t, content, `model_auto_compact_token_limit = 64000`)
	assert.Contains(t, content, `[model_providers.agentapi_openai_compatible]`)
	assert.Contains(t, content, `base_url = "http://ollama:11434/v1"`)
	assert.Contains(t, content, `wire_api = "responses"`)
	assert.Contains(t, content, `env_key = "OPENAI_API_KEY"`)
	assert.NotContains(t, content, "test-api-key")
}

func TestCompile_CodexConfigTOML_AppendsCustomProviderAfterExistingConfigTOML(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-custom-provider-append",
			UserID:    "user-codex-custom-provider-append",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Env: map[string]string{
			"OPENAI_BASE_URL":                          "http://proxy.example.com/v1",
			"OPENAI_MODEL":                             "gpt-oss:20b",
			"CODEX_MODEL":                              "qwen3-coder-next",
			"CODEX_MODEL_CONTEXT_WINDOW":               "65536",
			"CODEX_MODEL_AUTO_COMPACT_TOKEN_LIMIT":     "32768",
			"CODEX_MODEL_SUPPORTS_REASONING_SUMMARIES": "false",
		},
		Codex: CodexConfig{
			ConfigTOML: "approval-mode = \"full-auto\"\nmodel = \"gpt-5.5\"\nmodel_context_window = 128000\nmodel_auto_compact_token_limit = 64000\nmodel_supports_reasoning_summaries = true\nmodel_provider = \"openai\"\nsandbox_mode = \"danger-full-access\"\n\n[sandbox_workspace_write]\nnetwork_access = true\n",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "approval-mode")
	assert.Contains(t, content, `model_provider = "agentapi_openai_compatible"`)
	assert.Contains(t, content, `model = "qwen3-coder-next"`)
	assert.Contains(t, content, `model_context_window = 65536`)
	assert.Contains(t, content, `model_auto_compact_token_limit = 32768`)
	assert.Contains(t, content, `model_supports_reasoning_summaries = false`)
	assert.NotContains(t, content, `model_provider = "openai"`)
	assert.NotContains(t, content, `model = "gpt-5.5"`)
	assert.NotContains(t, content, `model = "gpt-oss:20b"`)
	assert.NotContains(t, content, `model_context_window = 128000`)
	assert.NotContains(t, content, `model_auto_compact_token_limit = 64000`)
	assert.NotContains(t, content, `model_supports_reasoning_summaries = true`)
	assert.Less(t, strings.Index(content, `model = "qwen3-coder-next"`), strings.Index(content, "[sandbox_workspace_write]"))
	assert.Less(t, strings.Index(content, `model_provider = "agentapi_openai_compatible"`), strings.Index(content, "[sandbox_workspace_write]"))
	assert.Less(t, strings.Index(content, "[sandbox_workspace_write]"), strings.Index(content, "[model_providers.agentapi_openai_compatible]"))
	assert.NotContains(t, content, "env_key")
}

func TestCompile_CodexConfigTOML_PreservesExistingModelWhenModelEnvIsNotSet(t *testing.T) {
	tmpDir := t.TempDir()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-codex-custom-provider-preserve-model",
			UserID:    "user-codex-custom-provider-preserve-model",
			Scope:     "user",
			AgentType: "codex-acp",
		},
		Env: map[string]string{
			"OPENAI_BASE_URL": "http://proxy.example.com/v1",
		},
		Codex: CodexConfig{
			ConfigTOML: "model = \"gpt-oss:20b\"\nmodel_provider = \"openai\"\n",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	configPath := filepath.Join(outputDir, ".codex/config.toml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `model = "gpt-oss:20b"`)
	assert.Contains(t, content, `model_provider = "agentapi_openai_compatible"`)
	assert.NotContains(t, content, `model_provider = "openai"`)
}
