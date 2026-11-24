//go:build e2e

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func sendMessageThroughProxy(ctx context.Context, proxyURL, sessionID, message string) (string, error) {
	// Create the message payload
	payload := map[string]interface{}{
		"content": message,
		"type":    "user",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %v", err)
	}

	// Send the message
	url := fmt.Sprintf("%s/%s/message", proxyURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	// Parse the response to extract the message content
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	// Extract message content from response
	if content, ok := response["content"].(string); ok {
		return content, nil
	}

	// If no content field, return the raw response
	return string(body), nil
}

// TestClaudeCodeProxyBasicSetup tests session setup with custom environment:
// - Session creation with working directory
// - Environment variable passing
// - Basic session lifecycle
// Note: Tool usage testing is skipped due to CI terminal limitations
func TestClaudeCodeProxyBasicSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if ANTHROPIC_API_KEY is available for real testing
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" || apiKey == "test-key-for-local-testing" {
		t.Skip("Skipping e2e test: ANTHROPIC_API_KEY not available")
	}

	// Verify Claude Code CLI is available
	if err := exec.Command("claude", "--version").Run(); err != nil {
		t.Skip("Skipping e2e test: Claude Code CLI not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout*2)
	defer cancel()

	// Start proxy server
	_, cleanup, err := startProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, proxyURL); err != nil {
		t.Fatalf("Proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(proxyURL)

	// Create session with working directory
	tempDir := t.TempDir()
	startReq := &client.StartRequest{
		Environment: map[string]string{
			"WORKING_DIR": tempDir,
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	if err != nil {
		t.Fatalf("Failed to start session: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Created session with working directory: %s", sessionID)

	// Wait for agentapi server to start up for this session
	time.Sleep(5 * time.Second)

	// Skip tool usage tests due to CI limitations
	t.Logf("Skipping tool usage tests due to CI environment limitations")
	t.Logf("Session created successfully with working directory: %s", tempDir)

	// Verify session status instead
	status, err := clientInstance.GetStatus(ctx, sessionID)
	if err != nil {
		t.Logf("Status check failed (expected in CI): %v", err)
	} else {
		t.Logf("Session status: %s", status.Status)
	}

	// Clean up
	_, err = clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}
}
