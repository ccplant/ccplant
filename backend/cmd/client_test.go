package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func captureClientOutput(t *testing.T, action func()) string {
	t.Helper()
	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	assert.NoError(t, err)
	os.Stdout = writer
	action()
	assert.NoError(t, writer.Close())
	os.Stdout = oldStdout
	output, err := io.ReadAll(reader)
	assert.NoError(t, err)
	return string(output)
}

func TestClientCmd(t *testing.T) {
	// Test command structure
	assert.Equal(t, "client", ClientCmd.Use)
	assert.Equal(t, "AgentAPI Client CLI", ClientCmd.Short)
}

func TestSendNotificationWithRetryRetries503(t *testing.T) {
	attempts := 0
	waits := 0
	response, err := sendNotificationWithRetry(context.Background(), func(context.Context) (*client.SendNotificationResponse, error) {
		attempts++
		if attempts < 3 {
			return nil, &client.HTTPStatusError{StatusCode: http.StatusServiceUnavailable, Body: "unavailable"}
		}
		return &client.SendNotificationResponse{Success: true}, nil
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 2, waits)
}

func TestSendNotificationWithRetryDoesNotRetry404(t *testing.T) {
	attempts := 0
	_, err := sendNotificationWithRetry(context.Background(), func(context.Context) (*client.SendNotificationResponse, error) {
		attempts++
		return nil, &client.HTTPStatusError{StatusCode: http.StatusNotFound, Body: "missing"}
	}, func(context.Context, time.Duration) error {
		t.Fatal("wait called for non-retryable response")
		return nil
	})

	assert.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestConsumeSecretPrintsOnlyValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, err := w.Write([]byte(`{"value":"injected-secret"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()
	var output bytes.Buffer
	err := consumeSecret(context.Background(), server.Client(), server.URL, &output)
	assert.NoError(t, err)
	assert.Equal(t, "injected-secret\n", output.String())
}

func TestClientCmdInit(t *testing.T) {
	// Test that subcommands are properly registered
	// Note: init() is called automatically when package is loaded

	// Test that subcommands are properly registered
	subcommands := ClientCmd.Commands()

	var commandNames []string
	for _, cmd := range subcommands {
		commandNames = append(commandNames, cmd.Use)
	}

	assert.Contains(t, commandNames, "send [message]")
	assert.Contains(t, commandNames, "history")
	assert.Contains(t, commandNames, "status")
	assert.Contains(t, commandNames, "events")
}

func TestClientCmdFlags(t *testing.T) {
	// Test persistent flags

	// Test persistent flags
	endpointFlag := ClientCmd.PersistentFlags().Lookup("endpoint")
	assert.NotNil(t, endpointFlag)
	assert.Equal(t, "e", endpointFlag.Shorthand)

	sessionFlag := ClientCmd.PersistentFlags().Lookup("session-id")
	assert.NotNil(t, sessionFlag)
	assert.Equal(t, "s", sessionFlag.Shorthand)
}

func TestSendCmd(t *testing.T) {
	assert.Equal(t, "send [message]", sendCmd.Use)
	assert.Equal(t, "Send a message to the agent", sendCmd.Short)
	assert.NotNil(t, sendCmd.Run)
}

func TestHistoryCmd(t *testing.T) {
	assert.Equal(t, "history", historyCmd.Use)
	assert.Equal(t, "Get conversation history", historyCmd.Short)
	assert.NotNil(t, historyCmd.Run)
}

func TestStatusCmd(t *testing.T) {
	assert.Equal(t, "status", statusCmd.Use)
	assert.Equal(t, "Get agent status", statusCmd.Short)
	assert.NotNil(t, statusCmd.Run)
}

func TestEventsCmd(t *testing.T) {
	assert.Equal(t, "events", eventsCmd.Use)
	assert.Equal(t, "Monitor agent events", eventsCmd.Short)
	assert.NotNil(t, eventsCmd.Run)
}

func TestMessageStruct(t *testing.T) {
	msg := client.Message{
		Content: "test message",
		Type:    "text",
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var unmarshaled client.Message
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, msg.Content, unmarshaled.Content)
	assert.Equal(t, msg.Type, unmarshaled.Type)
}

func TestMessageResponseStruct(t *testing.T) {
	resp := client.MessageResponse{
		OK: true,
	}

	data, err := json.Marshal(resp)
	assert.NoError(t, err)

	var unmarshaled client.MessageResponse
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, resp.OK, unmarshaled.OK)
	assert.True(t, unmarshaled.OK)
}

func TestStatusResponseStruct(t *testing.T) {
	status := client.StatusResponse{
		Status: "active",
	}

	data, err := json.Marshal(status)
	assert.NoError(t, err)

	var unmarshaled client.StatusResponse
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, status.Status, unmarshaled.Status)
}

func TestRunSendWithArgument(t *testing.T) {
	// Create a test server that mimics the agent API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/message") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var msg client.Message
		err := json.NewDecoder(r.Body).Decode(&msg)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Return a mock response
		response := client.MessageResponse{
			OK: true,
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	output := captureClientOutput(t, func() {
		runSend(&cobra.Command{}, []string{"test message"})
	})

	// Check if output contains expected response (actual output format may differ)
	assert.Contains(t, output, "Message sent successfully")
}

func TestRunHistoryWithMockServer(t *testing.T) {
	// Create a test server that returns mock history
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/messages") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response := client.MessagesResponse{
			Messages: []client.Message{
				{
					ID:        json.RawMessage(`"msg-1"`),
					Role:      "user",
					Content:   "Hello",
					Timestamp: func() *time.Time { t := time.Now().Add(-5 * time.Minute); return &t }(),
				},
				{
					ID:        json.RawMessage(`"msg-2"`),
					Role:      "assistant",
					Content:   "Hi there!",
					Timestamp: func() *time.Time { t := time.Now(); return &t }(),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	output := captureClientOutput(t, func() {
		runHistory(&cobra.Command{}, []string{})
	})

	// Check if output contains expected messages
	assert.Contains(t, output, "Hello")
	assert.Contains(t, output, "Hi there!")
	assert.Contains(t, output, "user")
	assert.Contains(t, output, "assistant")
}

func TestRunStatusWithMockServer(t *testing.T) {
	// Create a test server that returns mock status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		status := client.StatusResponse{
			Status: "active",
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(status))
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	output := captureClientOutput(t, func() {
		runStatus(&cobra.Command{}, []string{})
	})

	// Check if output contains expected status
	assert.Contains(t, output, "active")
}

func TestRunEventsWithMockServer(t *testing.T) {
	// Create a test server that returns Server-Sent Events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/events") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send a few test events
		bytesWritten, err := fmt.Fprintf(w, "data: {\"type\":\"message\",\"content\":\"Test event 1\"}\n\n")
		assert.NoError(t, err)
		assert.Positive(t, bytesWritten)
		w.(http.Flusher).Flush()

		bytesWritten, err = fmt.Fprintf(w, "data: {\"type\":\"status\",\"content\":\"Agent is thinking\"}\n\n")
		assert.NoError(t, err)
		assert.Positive(t, bytesWritten)
		w.(http.Flusher).Flush()

		// Close connection after a short delay
		time.Sleep(10 * time.Millisecond)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Capture output
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stdout = w

	// Run the command in a goroutine with timeout
	done := make(chan bool)
	go func() {
		runEvents(&cobra.Command{}, []string{})
		done <- true
	}()

	// Wait for a short time then close
	select {
	case <-done:
		// Command completed
	case <-time.After(100 * time.Millisecond):
		// Timeout - this is expected for SSE streams
	}

	// Restore stdout and read output
	assert.NoError(t, w.Close())
	os.Stdout = oldStdout

	outputBytes, err := io.ReadAll(r)
	assert.NoError(t, err)
	output := string(outputBytes)

	// Check if output contains expected events
	// Note: The exact output format depends on the implementation
	// This test mainly verifies that the function doesn't panic and can handle SSE
	assert.NotEmpty(t, output)
}
