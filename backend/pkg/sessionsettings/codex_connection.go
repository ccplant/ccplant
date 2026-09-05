package sessionsettings

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func structuredCodexProviderTOML(c *modelprovider.Connection) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model = %s\nmodel_provider = %s\n", tomlString(c.Model), tomlString(codexCustomOpenAIProviderID))
	if c.ContextWindow != nil {
		fmt.Fprintf(&b, "model_context_window = %d\n", *c.ContextWindow)
	}
	if c.AutoCompactTokenLimit != nil {
		fmt.Fprintf(&b, "model_auto_compact_token_limit = %d\n", *c.AutoCompactTokenLimit)
	}
	if c.SupportsReasoningSummaries != nil {
		fmt.Fprintf(&b, "model_supports_reasoning_summaries = %t\n", *c.SupportsReasoningSummaries)
	}
	fmt.Fprintf(&b, "\n[model_providers.%s]\nname = \"OpenAI compatible\"\nbase_url = %s\nwire_api = \"responses\"\nrequires_openai_auth = false\n", codexCustomOpenAIProviderID, tomlString(c.BaseURL))
	if c.Authentication != "none" {
		b.WriteString("env_key = \"CCPLANT_CODEX_API_KEY\"\n")
	}
	return b.String()
}

// MergeCodexConnectionConfig replaces the managed provider without duplicating TOML tables.
func MergeCodexConnectionConfig(base string, c *modelprovider.Connection) (string, error) {
	config := map[string]interface{}{}
	if err := toml.Unmarshal([]byte(base), &config); err != nil {
		return "", fmt.Errorf("invalid Codex config TOML: %w", err)
	}
	providers, _ := config["model_providers"].(map[string]interface{})
	if providers == nil {
		providers = map[string]interface{}{}
	}
	delete(providers, codexCustomOpenAIProviderID)
	if c.Compatible() {
		generated := map[string]interface{}{}
		if err := toml.Unmarshal([]byte(structuredCodexProviderTOML(c)), &generated); err != nil {
			return "", err
		}
		for _, key := range []string{"model_context_window", "model_auto_compact_token_limit", "model_supports_reasoning_summaries"} {
			delete(config, key)
		}
		for key, value := range generated {
			if key != "model_providers" {
				config[key] = value
			}
		}
		providers[codexCustomOpenAIProviderID] = generated["model_providers"].(map[string]interface{})[codexCustomOpenAIProviderID]
	} else {
		if config["model_provider"] == codexCustomOpenAIProviderID {
			delete(config, "model")
		}
		config["model_provider"] = "openai"
	}
	config["model_providers"] = providers
	data, err := toml.Marshal(config)
	return string(data), err
}
