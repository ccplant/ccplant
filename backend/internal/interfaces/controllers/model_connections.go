package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
)

func mergeModelConnection(existing *modelprovider.Connection, patch map[string]json.RawMessage, agent string) (*modelprovider.Connection, error) {
	if patch == nil {
		return existing, nil
	}
	if _, ok := patch["mode"]; !ok {
		return nil, fmt.Errorf("connection mode is required")
	}
	c := existing.Clone()
	if c == nil {
		c = &modelprovider.Connection{}
	}
	data, _ := json.Marshal(c)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	var key *string
	clear := false
	for k, v := range patch {
		switch k {
		case "api_key":
			if string(v) == "null" {
				return nil, fmt.Errorf("api_key cannot be null")
			}
			if err := json.Unmarshal(v, &key); err != nil {
				return nil, fmt.Errorf("invalid api_key")
			}
		case "clear_api_key":
			if err := json.Unmarshal(v, &clear); err != nil {
				return nil, fmt.Errorf("invalid clear_api_key")
			}
		case "has_api_key": // read-only metadata may be round-tripped by clients
		case "mode", "base_url", "model", "authentication", "context_window", "auto_compact_token_limit", "supports_reasoning_summaries", "model_aliases":
			if (k == "mode" || k == "base_url" || k == "model" || k == "authentication") && string(v) == "null" {
				return nil, fmt.Errorf("%s cannot be null", k)
			}
			fields[k] = v
		default:
			return nil, fmt.Errorf("unknown connection field: %s", k)
		}
	}
	if key != nil && (clear || *key == "") {
		return nil, fmt.Errorf("provide a non-empty api_key or clear_api_key, not both")
	}
	data, _ = json.Marshal(fields)
	c.ModelAliases = nil
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("invalid connection fields")
	}
	if key != nil {
		c.APIKey = *key
	}
	if clear {
		c.APIKey = ""
	}
	c.HasAPIKey = c.APIKey != ""
	if err := c.Validate(agent); err != nil {
		return nil, err
	}
	return c, nil
}

func updateModelConnections(settings *entities.Settings, req *UpdateSettingsRequest) error {
	codex, err := mergeModelConnection(settings.CodexConnection(), req.CodexConnection, "codex")
	if err != nil {
		return err
	}
	claude, err := mergeModelConnection(settings.ClaudeConnection(), req.ClaudeConnection, "claude")
	if err != nil {
		return err
	}
	if req.AuthMode != nil && claude != nil {
		if req.ClaudeConnection != nil && *req.AuthMode != claude.Mode {
			return fmt.Errorf("auth_mode conflicts with claude_connection.mode")
		}
		if req.ClaudeConnection == nil {
			claude.Mode = *req.AuthMode
			if err := claude.Validate("claude"); err != nil {
				return err
			}
		}
	}
	if req.AuthMode != nil && *req.AuthMode == "anthropic_compatible" && claude == nil {
		return fmt.Errorf("claude_connection is required")
	}
	settings.SetCodexConnection(codex)
	settings.SetClaudeConnection(claude)
	return nil
}
