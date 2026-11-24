package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func modelValueToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]interface{}:
		for _, key := range []string{"id", "model", "modelId", "name", "displayName", "value", "default"} {
			if model := modelValueToString(v[key]); model != "" {
				return model
			}
		}
	case []interface{}:
		if len(v) == 1 {
			return modelValueToString(v[0])
		}
	}
	return ""
}

// Prompt sends a text user message and returns when the agent has finished the turn.
// While the agent is working, session/update notifications are dispatched to
// the channel returned by Updates().
func (c *Client) Prompt(ctx context.Context, text string) (StopReason, error) {
	return c.PromptBlocks(ctx, []ContentBlock{{Type: ContentBlockTypeText, Text: text}})
}

// PromptBlocks sends an ordered multimodal prompt and returns when the agent
// has finished the turn.
func (c *Client) PromptBlocks(ctx context.Context, prompt []ContentBlock) (StopReason, error) {
	sessionId := c.SessionID()
	if sessionId == "" {
		return "", fmt.Errorf("acp: no active session; call NewSession first")
	}
	if len(prompt) == 0 {
		return "", fmt.Errorf("acp: prompt must contain at least one content block")
	}
	params := PromptParams{
		SessionId: sessionId,
		Prompt:    prompt,
	}
	raw, err := c.rpc.Call(ctx, "session/prompt", params)
	if err != nil {
		return StopReasonCancelled, fmt.Errorf("acp session/prompt: %w", err)
	}
	var result PromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("acp session/prompt: parse result: %w", err)
	}
	return result.StopReason, nil
}

// SetSessionConfigOption changes a runtime session configuration option.
func (c *Client) SetSessionConfigOption(ctx context.Context, configId, value string) (SessionSetConfigOptionResult, error) {
	sessionId := c.SessionID()
	if sessionId == "" {
		return SessionSetConfigOptionResult{}, fmt.Errorf("acp: no active session; call NewSession first")
	}
	params := SessionSetConfigOptionParams{
		SessionId: sessionId,
		ConfigId:  configId,
		Value:     value,
	}
	raw, err := c.rpc.Call(ctx, "session/set_config_option", params)
	if err != nil {
		return SessionSetConfigOptionResult{}, fmt.Errorf("acp session/set_config_option: %w", err)
	}
	var result SessionSetConfigOptionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return SessionSetConfigOptionResult{}, fmt.Errorf("acp session/set_config_option: parse result: %w", err)
	}
	c.updateSessionConfigOptions(result.ConfigOptions)
	return result, nil
}

// Cancel sends a session/cancel notification to abort the current turn.
func (c *Client) Cancel(ctx context.Context) error {
	sessionId := c.SessionID()
	if sessionId == "" {
		return nil
	}
	return c.rpc.Notify("session/cancel", SessionCancelParams{SessionId: sessionId})
}

// Updates returns a channel that receives session/update notifications from
// the agent. The channel is closed when the client stops.
func (c *Client) Updates() <-chan SessionUpdate {
	return c.updateCh
}

// PermissionRequests returns a channel that receives inbound permission
// requests from the agent.
func (c *Client) PermissionRequests() <-chan PermissionRequest {
	return c.permCh
}
