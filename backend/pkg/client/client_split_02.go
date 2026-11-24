package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"io"
	"net/http"
	"time"
)

func (c *Client) GetSessionByID(ctx context.Context, sessionID string) (*SessionInfo, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	resp, err := c.Search(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to search sessions: %w", err)
	}

	for i := range resp.Sessions {
		if resp.Sessions[i].SessionID == sessionID {
			return &resp.Sessions[i], nil
		}
	}

	return nil, fmt.Errorf("session %s not found", sessionID)
}

// DeleteSession terminates and deletes a session
func (c *Client) DeleteSession(ctx context.Context, sessionID string) (*DeleteResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	url := fmt.Sprintf("%s/sessions/%s", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
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
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var deleteResp DeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &deleteResp, nil
}

// UpdateSessionAnnotations updates user-managed annotations for a session.
func (c *Client) UpdateSessionAnnotations(ctx context.Context, sessionID string, req *UpdateSessionAnnotationsRequest) (*UpdateSessionAnnotationsResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqURL := fmt.Sprintf("%s/sessions/%s/annotations", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var updateResp UpdateSessionAnnotationsResponse
	if err := json.NewDecoder(resp.Body).Decode(&updateResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &updateResp, nil
}

// SendMessage sends a message to an agentapi session
func (c *Client) SendMessage(ctx context.Context, sessionID string, message *Message) (*MessageResponse, error) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	url := fmt.Sprintf("%s/%s/message", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var msgResp MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &msgResp, nil
}

// GetMessages retrieves conversation history from an agentapi session
func (c *Client) GetMessages(ctx context.Context, sessionID string) (*MessagesResponse, error) {
	url := fmt.Sprintf("%s/%s/messages", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var messagesResp MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&messagesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &messagesResp, nil
}

// GetStatus retrieves the current agent status from an agentapi session
func (c *Client) GetStatus(ctx context.Context, sessionID string) (*StatusResponse, error) {
	url := fmt.Sprintf("%s/%s/status", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
	}

	var statusResp StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &statusResp, nil
}

// SendNotificationRequest represents the request for sending a push notification via API
type SendNotificationRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// SendNotificationResponse represents the response for sending a push notification via API
type SendNotificationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// HTTPStatusError preserves the response status for callers that need to
// distinguish retryable failures from permanent request errors.
type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("server returned status %d: %s", e.StatusCode, e.Body)
}

// SendNotification sends a push notification via the API.
// Either SessionID or UserID must be specified.
func (c *Client) SendNotification(ctx context.Context, req *SendNotificationRequest) (*SendNotificationResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/notifications/send", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		closeutils.SafeCloseResponseBody(resp.Body)
	}()

	if resp.StatusCode != http.StatusOK {
		body, err := readResponseBody(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: body}
	}

	var sendResp SendNotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &sendResp, nil
}

// StreamEvents subscribes to Server-Sent Events (SSE) from an agentapi session
func (c *Client) StreamEvents(ctx context.Context, sessionID string) (<-chan string, <-chan error) {
	eventChan := make(chan string, 100)
	errorChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errorChan)

		url := fmt.Sprintf("%s/%s/events", c.baseURL, sessionID)
		httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
		defer func() {
			closeutils.SafeCloseResponseBody(resp.Body)
		}()

		if resp.StatusCode != http.StatusOK {
			body, err := readResponseBody(resp.Body)
			if err != nil {
				errorChan <- err
				return
			}
			errorChan <- fmt.Errorf("server returned status %d: %s", resp.StatusCode, body)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			select {
			case eventChan <- line:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading response: %w", err)
		}
	}()

	return eventChan, errorChan
}

func readResponseBody(reader io.Reader) (string, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read error response: %w", err)
	}
	return string(body), nil
}

// ProxySessionStatusEvent is a proxy-level event emitted whenever any session's status changes.
type ProxySessionStatusEvent struct {
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// WatchSessionsStatus subscribes to proxy-wide session status changes via SSE.
// The returned channel receives a ProxySessionStatusEvent whenever any accessible session
// changes status. The caller must cancel ctx to stop the stream; both channels are closed
// when the stream ends.
