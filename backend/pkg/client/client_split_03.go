package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) WatchSessionsStatus(ctx context.Context) (<-chan ProxySessionStatusEvent, <-chan error) {
	eventChan := make(chan ProxySessionStatusEvent, 32)
	errorChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errorChan)

		reqURL := fmt.Sprintf("%s/sessions/status/stream", c.baseURL)
		httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")

		if err := c.applyMiddlewares(httpReq); err != nil {
			errorChan <- err
			return
		}

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errorChan <- fmt.Errorf("failed to send request: %w", err)
			return
		}
		defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue // skip heartbeat comments and blank lines
			}
			var evt ProxySessionStatusEvent
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt); err != nil {
				continue // skip malformed events
			}
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading stream: %w", err)
		}
	}()

	return eventChan, errorChan
}

// WaitSessionsStatus long-polls for the next proxy-wide session status change.
// It blocks until any accessible session changes status or until the timeout elapses.
// Returns the event on change, or (nil, nil) on timeout.
// timeoutSec=0 uses the server default (30 s). Valid range: 1–60.
func (c *Client) WaitSessionsStatus(ctx context.Context, timeoutSec int) (*ProxySessionStatusEvent, error) {
	u, err := url.Parse(fmt.Sprintf("%s/sessions/status/wait", c.baseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	if timeoutSec > 0 {
		q := u.Query()
		q.Set("timeout", fmt.Sprintf("%d", timeoutSec))
		u.RawQuery = q.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Timeout response: {"events": []}
	var evt ProxySessionStatusEvent
	if err := json.Unmarshal(body, &evt); err != nil || evt.SessionID == "" {
		return nil, nil
	}
	return &evt, nil
}
