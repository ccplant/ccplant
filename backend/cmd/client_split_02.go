package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func runCycle(cmd *cobra.Command, args []string) error {
	// Read /tmp/check/CYCLE_ENABLED. Its content is the message to send.
	// If the file does not exist the cycle is disabled — exit without doing anything.
	messageBytes, err := os.ReadFile(cycleEnabledPath)
	if os.IsNotExist(err) {
		fmt.Println("CYCLE_ENABLED not found, skipping cycle")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", cycleEnabledPath, err)
	}

	message := strings.TrimSpace(string(messageBytes))
	if message == "" {
		return fmt.Errorf("%s exists but is empty; write the cycle message into it", cycleEnabledPath)
	}

	// Read and check the cycle count when --max-count is set
	if cycleMaxCount > 0 {
		count, err := readCycleCount()
		if err != nil {
			return err
		}
		if count >= cycleMaxCount {
			fmt.Printf("Cycle count limit reached (%d/%d), removing CYCLE_ENABLED\n", count, cycleMaxCount)
			// Remove CYCLE_ENABLED so the hook becomes a no-op from now on.
			_ = os.Remove(cycleEnabledPath)
			return nil
		}
		// Increment and persist the counter before sending
		if err := writeCycleCount(count + 1); err != nil {
			return err
		}
		fmt.Printf("Cycle count: %d/%d\n", count+1, cycleMaxCount)
	}

	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		return fmt.Errorf("failed to resolve client: %w", err)
	}

	// Wait for the session to become stable before sending.
	// The Stop hook fires while Claude is still wrapping up ("running"),
	// and agentapi rejects user messages with 422 until the status is "stable".
	ctx := context.Background()
	if err := waitForStable(ctx, c, resolvedSessionID); err != nil {
		return fmt.Errorf("timed out waiting for stable status: %w", err)
	}

	msg := &client.Message{
		Content: message + cycleConditionCheckSuffix,
		Type:    "user",
	}

	msgResp, err := c.SendMessage(ctx, resolvedSessionID, msg)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}

	if msgResp.OK {
		fmt.Println("Message sent successfully")
	} else {
		return fmt.Errorf("message was not sent successfully")
	}

	return nil
}

// waitForStable polls the session status until it is "stable" or the timeout is reached.
func waitForStable(ctx context.Context, c *client.Client, sessionID string) error {
	const (
		pollInterval = 2 * time.Second
		timeout      = 120 * time.Second
	)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statusResp, err := c.GetStatus(ctx, sessionID)
		if err == nil && statusResp.Status == "stable" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("session did not become stable within %s", timeout)
}

func runSend(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var message string
	if len(args) > 0 {
		message = args[0]
	} else {
		fmt.Print("Enter message: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			message = scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}
	}

	if message == "" {
		fmt.Fprintf(os.Stderr, "Message cannot be empty\n")
		return
	}

	ctx := context.Background()

	msg := &client.Message{
		Content: message,
		Type:    "user",
	}

	msgResp, err := c.SendMessage(ctx, resolvedSessionID, msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		return
	}

	if msgResp.OK {
		fmt.Printf("Message sent successfully\n")
	} else {
		fmt.Fprintf(os.Stderr, "Message was not sent successfully\n")
	}
}

func runHistory(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	messagesResp, err := c.GetMessages(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting history: %v\n", err)
		return
	}

	fmt.Printf("Conversation History (%d messages):\n", len(messagesResp.Messages))
	for _, msg := range messagesResp.Messages {
		ts := ""
		if msg.Timestamp != nil {
			ts = msg.Timestamp.Format("15:04:05")
		}
		fmt.Printf("[%s] %s: %s\n", ts, msg.Role, msg.Content)
	}
}

func runStatus(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	statusResp, err := c.GetStatus(ctx, resolvedSessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting status: %v\n", err)
		return
	}

	fmt.Printf("Agent Status: %s\n", statusResp.Status)
}

func runEvents(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	eventChan, errorChan := c.StreamEvents(ctx, resolvedSessionID)

	fmt.Println("Monitoring events... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			if strings.HasPrefix(event, "data: ") {
				data := strings.TrimPrefix(event, "data: ")
				fmt.Printf("[EVENT] %s\n", data)
			}
		case err, ok := <-errorChan:
			if !ok {
				return
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading events: %v\n", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func runAnnotateSession(cmd *cobra.Command, args []string) {
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	req := &client.UpdateSessionAnnotationsRequest{}
	changed := false
	if cmd.Flags().Changed("pr-url") {
		req.PRURL = &annotationPRURL
		changed = true
	}
	if cmd.Flags().Changed("issue-url") {
		req.IssueURL = &annotationIssueURL
		changed = true
	}
	if cmd.Flags().Changed("description") {
		req.Description = &annotationDescription
		changed = true
	}
	if cmd.Flags().Changed("running-task") {
		req.RunningTask = &annotationRunningTask
		changed = true
	}
	if !changed {
		fmt.Fprintln(os.Stderr, "Error: at least one annotation flag is required")
		os.Exit(1)
	}

	resp, err := c.UpdateSessionAnnotations(context.Background(), resolvedSessionID, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error updating session annotations: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// readJSONInput reads JSON from a file path, or from stdin if path is "-" or empty.
// Returns the raw bytes to be used as an HTTP request body.
func readJSONInput(file string) ([]byte, error) {
	if file == "-" || file == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", file, err)
	}
	return data, nil
}

// prettyJSONOutput pretty-prints raw JSON bytes. Falls back to the original bytes if parsing fails.
func prettyJSONOutput(data []byte) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	if err := enc.Encode(v); err != nil {
		return string(data)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// endpointHint is appended to error messages when the endpoint cannot be resolved,
// to help users understand how to configure the connection.
const endpointHint = `
Hint: configure the endpoint using one of the following methods:
  1. Flag:    --endpoint http://<host>:<port>
  2. Env vars: AGENTAPI_PROXY_SERVICE_HOST=<host> AGENTAPI_PROXY_SERVICE_PORT_HTTP=<port>
  3. Config:  ~/.ccplant/config.yaml (api_endpoint)

Optional authentication:
  AGENTAPI_KEY=<api-key> or config api_token/api_token_helper`

// resolveBaseClient creates a client using flags, environment variables, or the
// user CLI config.
// Unlike resolveClient, no session-id is required.
func resolveBaseClient() (*client.Client, error) {
	resolvedEndpoint, apiKey, err := resolveCLIConnection(context.Background(), endpoint, "")
	if err != nil {
		return nil, err
	}
	return client.NewClient(resolvedEndpoint, client.WithAPIKeyAuth(apiKey)), nil
}

// readContentFlag reads content from a flag or file.
func readContentFlag(content, contentFile string) (string, error) {
	if contentFile != "" {
		if contentFile == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("failed to read stdin: %w", err)
			}
			return string(data), nil
		}
		data, err := os.ReadFile(contentFile)
		if err != nil {
			return "", fmt.Errorf("failed to read content file %q: %w", contentFile, err)
		}
		return string(data), nil
	}
	return content, nil
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	c, config, err := client.NewClientFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !confirmDelete {
		fmt.Printf("Are you sure you want to delete session %s? [y/N]: ", config.SessionID)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			response := strings.ToLower(strings.TrimSpace(scanner.Text()))
			if response != "y" && response != "yes" {
				fmt.Println("Deletion cancelled")
				return
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
			return
		}
	}
	resp, err := c.DeleteSession(context.Background(), config.SessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Session deleted successfully: %s\n", resp.Message)
	fmt.Printf("Session ID: %s\n", resp.SessionID)
}
