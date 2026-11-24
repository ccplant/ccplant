package slackbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func (r *SlackChannelResolver) PostMessage(ctx context.Context, channel, threadTS, text, botToken string) error {
	if botToken == "" {
		return fmt.Errorf("bot token is empty; cannot post message to Slack")
	}

	payload := postMessageRequest{
		Channel:  channel,
		Text:     text,
		ThreadTS: threadTS,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.slackAPIBase+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack API request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[CHANNEL_RESOLVER] Failed to close PostMessage response body: %v", closeErr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Slack API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result postMessageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse Slack API response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	log.Printf("[CHANNEL_RESOLVER] Posted message to channel=%s thread=%s", channel, threadTS)
	return nil
}
