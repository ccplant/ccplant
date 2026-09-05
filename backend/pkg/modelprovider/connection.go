// Package modelprovider defines structured, agent-specific API connections.
package modelprovider

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

type Connection struct {
	Mode                       string            `json:"mode"`
	BaseURL                    string            `json:"base_url,omitempty"`
	Model                      string            `json:"model,omitempty"`
	Authentication             string            `json:"authentication,omitempty"`
	APIKey                     string            `json:"-" yaml:"-"`
	HasAPIKey                  bool              `json:"has_api_key"`
	ContextWindow              *int64            `json:"context_window,omitempty"`
	AutoCompactTokenLimit      *int64            `json:"auto_compact_token_limit,omitempty"`
	SupportsReasoningSummaries *bool             `json:"supports_reasoning_summaries,omitempty"`
	ModelAliases               map[string]string `json:"model_aliases,omitempty"`
}

func (c *Connection) Clone() *Connection {
	if c == nil {
		return nil
	}
	copy := *c
	if c.ContextWindow != nil {
		value := *c.ContextWindow
		copy.ContextWindow = &value
	}
	if c.AutoCompactTokenLimit != nil {
		value := *c.AutoCompactTokenLimit
		copy.AutoCompactTokenLimit = &value
	}
	if c.SupportsReasoningSummaries != nil {
		value := *c.SupportsReasoningSummaries
		copy.SupportsReasoningSummaries = &value
	}
	if c.ModelAliases != nil {
		copy.ModelAliases = map[string]string{}
		for k, v := range c.ModelAliases {
			copy.ModelAliases[k] = v
		}
	}
	copy.HasAPIKey = c.APIKey != ""
	return &copy
}

func (c *Connection) Compatible() bool {
	return c != nil && (c.Mode == "openai_compatible" || c.Mode == "anthropic_compatible")
}

func ValidateModel(model string) error {
	if strings.TrimSpace(model) == "" || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return fmt.Errorf("model must be a non-empty model ID without control characters")
	}
	return nil
}

func (c *Connection) Validate(agent string) error {
	if c == nil {
		return nil
	}
	switch agent {
	case "codex":
		if c.Mode != "auth_json" && c.Mode != "openai_compatible" {
			return fmt.Errorf("invalid Codex connection mode")
		}
		if len(c.ModelAliases) > 0 {
			return fmt.Errorf("model_aliases is only supported for Claude Code")
		}
	case "claude":
		if c.Mode != "oauth" && c.Mode != "bedrock" && c.Mode != "anthropic_compatible" {
			return fmt.Errorf("invalid Claude connection mode")
		}
		if c.ContextWindow != nil || c.AutoCompactTokenLimit != nil || c.SupportsReasoningSummaries != nil {
			return fmt.Errorf("Codex model metadata is not supported for Claude Code")
		}
	default:
		return fmt.Errorf("invalid agent")
	}
	if !c.Compatible() {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("base_url must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/responses", "/chat/completions", "/messages", "/messages/count_tokens"} {
		if strings.HasSuffix(path, suffix) {
			return fmt.Errorf("base_url must be an API base, not an endpoint")
		}
	}
	if agent == "claude" && strings.HasSuffix(path, "/v1") {
		return fmt.Errorf("Claude base_url must omit the trailing /v1")
	}
	if err := ValidateModel(c.Model); err != nil {
		return err
	}
	validAuthentication := c.Authentication == "api_key" || (agent == "codex" && c.Authentication == "none") || (agent == "claude" && c.Authentication == "bearer_token")
	if !validAuthentication {
		return fmt.Errorf("invalid connection authentication")
	}
	if c.Authentication != "none" && strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("API key is required")
	}
	if strings.IndexFunc(c.APIKey, unicode.IsControl) >= 0 {
		return fmt.Errorf("API key must not contain control characters")
	}
	if c.ContextWindow != nil && (*c.ContextWindow <= 0 || *c.ContextWindow > 1000000000) {
		return fmt.Errorf("invalid context_window")
	}
	if c.AutoCompactTokenLimit != nil && (c.ContextWindow == nil || *c.AutoCompactTokenLimit <= 0 || *c.AutoCompactTokenLimit >= *c.ContextWindow) {
		return fmt.Errorf("auto_compact_token_limit must be positive and below context_window")
	}
	for k, v := range c.ModelAliases {
		if k != "sonnet" && k != "opus" && k != "haiku" {
			return fmt.Errorf("invalid model alias")
		}
		if err := ValidateModel(v); err != nil {
			return err
		}
	}
	return nil
}

// ModelForLayers resolves model-only overrides without mixing connection credentials.
func ModelForLayers(agent, fallback string, layers ...map[string]string) string {
	model := fallback
	for _, env := range layers {
		if agent == "codex" {
			if v := strings.TrimSpace(env["OPENAI_MODEL"]); v != "" {
				model = v
			}
			if v := strings.TrimSpace(env["CODEX_MODEL"]); v != "" {
				model = v
			}
		} else if v := strings.TrimSpace(env["ANTHROPIC_MODEL"]); v != "" {
			model = v
		}
	}
	return model
}
