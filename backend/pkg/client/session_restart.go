package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/url"
)

type SessionRestartStatus struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	Phase     string `json:"phase"`
	Revision  int64  `json:"revision"`
	Error     string `json:"error,omitempty"`
}

func (c *Client) SessionLifecycle(ctx context.Context, id, action string, reload bool, startupInputs ...json.RawMessage) (*SessionRestartStatus, error) {
	method := http.MethodPost
	path := action
	if action == "restart-status" {
		method = http.MethodGet
		path = "restart"
	}
	input := map[string]interface{}{"reload_settings": reload, "busy_policy": "wait"}
	if len(startupInputs) > 0 {
		input["startup_input"] = startupInputs[0]
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/sessions/"+url.PathEscape(id)+"/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	if err := c.applyMiddlewares(req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("session %s failed: HTTP %d", action, resp.StatusCode)
	}
	var result SessionRestartStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
