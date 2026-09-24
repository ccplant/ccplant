package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
	"github.com/takutakahashi/agentapi-proxy/pkg/usagecollector"
)

var (
	endpoint      string
	sessionID     string
	confirmDelete bool
)

// annotate-session command flags
var (
	annotationPRURL       string
	annotationIssueURL    string
	annotationDescription string
	annotationRunningTask string
)

// asset subcommand flags
var (
	assetHTML     string
	assetHTMLFile string
	assetFormat   string
)

// send-notification subcommand flags
var (
	clientNotifyTitle     string
	clientNotifyBody      string
	clientNotifySessionID string
	clientNotifyUserID    string
)

// cycle subcommand flags
var (
	cycleMaxCount  int
	usageAgentType string
)

// resolveClient creates a client using flags if provided, otherwise falling back
// to environment variables (AGENTAPI_PROXY_SERVICE_HOST, AGENTAPI_PROXY_SERVICE_PORT_HTTP,
// AGENTAPI_SESSION_ID, AGENTAPI_KEY).
// Returns the client and the resolved session ID.
func resolveClient() (*client.Client, string, error) {
	resolvedEndpoint := endpoint
	resolvedSessionID := sessionID

	if resolvedEndpoint == "" {
		envEndpoint, err := client.EndpointFromEnv()
		if err != nil {
			return nil, "", fmt.Errorf("--endpoint not specified and %w", err)
		}
		resolvedEndpoint = envEndpoint
	}

	if resolvedSessionID == "" {
		resolvedSessionID = os.Getenv("AGENTAPI_SESSION_ID")
		if resolvedSessionID == "" {
			return nil, "", fmt.Errorf("--session-id not specified and AGENTAPI_SESSION_ID is not set")
		}
	}

	apiKey := os.Getenv("AGENTAPI_KEY")
	c := client.NewClient(resolvedEndpoint, client.WithAPIKeyAuth(apiKey))
	return c, resolvedSessionID, nil
}

var ClientCmd = &cobra.Command{
	Use:   "client",
	Short: "AgentAPI Client CLI",
	Long:  "Command line client for interacting with AgentAPI endpoints",
}

var cycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "Send a cycle message read from CYCLE_ENABLED; no-op if the file is absent",
	Long: `Read /tmp/check/CYCLE_ENABLED. If the file does not exist, exit without doing
anything (cycle is disabled).

The file content is used as the message sent to the session.  This allows the
Stop hook to be registered permanently in Claude's settings and the cycle to be
activated simply by writing the desired message into /tmp/check/CYCLE_ENABLED.

To stop the cycle, the agent (or a user) deletes /tmp/check/CYCLE_ENABLED.  The
auto-appended suffix included in every sent message instructs the agent to do this
when the goal is achieved.

Each invocation increments a counter stored in /tmp/check/CYCLE_COUNT.
If --max-count is set and the counter reaches the limit, the command exits without
sending a message and also removes CYCLE_ENABLED to prevent further cycles.

Examples:
  # Enable cycling by writing a message into the marker file
  mkdir -p /tmp/check
  echo "Please continue the task" > /tmp/check/CYCLE_ENABLED

  # Stop after 10 cycles at most
  agentapi-proxy client cycle --max-count 10

  agentapi-proxy client cycle --session-id my-session`,
	Args: cobra.NoArgs,
	RunE: runCycle,
}

var reportUsageCmd = &cobra.Command{
	Use:   "report-usage",
	Short: "Report token usage from a Stop hook transcript",
	Args:  cobra.NoArgs,
	RunE:  runReportUsage,
}

var consumeSecretCmd = &cobra.Command{
	Use:   "consume-secret",
	Short: "Consume the next one-time secret registered for this session",
	Args:  cobra.NoArgs,
	RunE:  runConsumeSecret,
}

var sendCmd = &cobra.Command{
	Use:   "send [message]",
	Short: "Send a message to the agent",
	Long:  "Send a message to the agent endpoint",
	Args:  cobra.MaximumNArgs(1),
	Run:   runSend,
}

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Get conversation history",
	Long:  "Retrieve the conversation history from the agent",
	Run:   runHistory,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get agent status",
	Long:  "Get the current status of the agent",
	Run:   runStatus,
}

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Monitor agent events",
	Long:  "Monitor real-time events from the agent using Server-Sent Events",
	Run:   runEvents,
}

var deleteSessionCmd = &cobra.Command{
	Use:   "delete-session",
	Short: "Delete the current session",
	Long: `Delete the current session using environment variables.

This command deletes the current agent session by reading configuration from
environment variables:
- AGENTAPI_SESSION_ID: The session ID to delete
- AGENTAPI_KEY: API key for authentication
- AGENTAPI_PROXY_SERVICE_HOST and AGENTAPI_PROXY_SERVICE_PORT_HTTP: For endpoint URL

Examples:
  # Delete current session (with confirmation)
  agentapi-proxy client delete-session

  # Delete current session without confirmation
  agentapi-proxy client delete-session --confirm`,
	Run: runDeleteSession,
}

var annotateSessionCmd = &cobra.Command{
	Use:   "annotate-session",
	Short: "Update current session info",
	Long: `Update the current session's user-managed info.

The supported fields are PR URL, issue URL, description, and running task.
At least one flag must be specified. Use an explicit empty string to clear a field.

Examples:
  agentapi-proxy client annotate-session \
    --endpoint http://proxy:8080 \
    --session-id my-session \
    --pr-url https://github.com/owner/repo/pull/123 \
    --issue-url https://github.com/owner/repo/issues/456 \
    --description "Session annotation support" \
    --running-task "Implement session annotations"

  # Clear the running task
  agentapi-proxy client annotate-session --running-task ""`,
	Run: runAnnotateSession,
}

var assetCmd = &cobra.Command{
	Use:   "asset",
	Short: "Manage static HTML assets",
	Long:  "Upload HTML and receive an externally reachable asset URL",
}

var assetCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Upload an HTML asset",
	Long:  "Upload HTML from --html, --html-file, or stdin and receive an externally reachable asset URL.",
	Run:   runAssetCreate,
}

var sendNotificationClientCmd = &cobra.Command{
	Use:   "send-notification",
	Short: "Send a push notification via API",
	Long: `Send a push notification to subscribers via the agentapi-proxy API.

Either --notify-session-id or --notify-user-id must be specified to identify the target.

Examples:
  # Send to all users subscribed to a session
  ccplant client send-notification \
    --title "作業が完了しました" \
    --body "作業内容を確認してください" \
    --notify-session-id "$AGENTAPI_SESSION_ID"

  # Send to a specific user
  ccplant client send-notification \
    --title "Notification" \
    --body "Something happened" \
    --notify-user-id "user123"`,
	RunE: runClientSendNotification,
}

func init() {
	ClientCmd.PersistentFlags().StringVarP(&endpoint, "endpoint", "e", "", "AgentAPI endpoint URL (required for most commands)")
	ClientCmd.PersistentFlags().StringVarP(&sessionID, "session-id", "s", "", "Session ID for the agent (required for most commands)")

	// delete-session command flags
	deleteSessionCmd.Flags().BoolVar(&confirmDelete, "confirm", false, "Skip confirmation prompt")

	// annotate-session command flags
	annotateSessionCmd.Flags().StringVar(&annotationPRURL, "pr-url", "", "Pull request URL annotation")
	annotateSessionCmd.Flags().StringVar(&annotationIssueURL, "issue-url", "", "Issue URL annotation")
	annotateSessionCmd.Flags().StringVar(&annotationDescription, "description", "", "Description annotation")
	annotateSessionCmd.Flags().StringVar(&annotationRunningTask, "running-task", "", "Running task annotation")

	// asset create flags
	assetCreateCmd.Flags().StringVar(&assetHTML, "html", "", "HTML content to upload")
	assetCreateCmd.Flags().StringVar(&assetHTMLFile, "html-file", "", "Path to file containing HTML content; use - for stdin")
	assetCreateCmd.Flags().StringVar(&assetFormat, "format", "url", `Output format: "url" or "json"`)

	assetCmd.AddCommand(assetCreateCmd)

	// send-notification flags
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyTitle, "title", "", "Notification title (required)")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyBody, "body", "", "Notification body (required)")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifySessionID, "notify-session-id", "", "Session ID whose subscribers will receive the notification")
	sendNotificationClientCmd.Flags().StringVar(&clientNotifyUserID, "notify-user-id", "", "User ID to send the notification to")

	// cycle flags
	cycleCmd.Flags().IntVar(&cycleMaxCount, "max-count", 10, "Maximum number of cycles (0 means unlimited, default: 10). Exits when the count in /tmp/check/CYCLE_COUNT reaches this limit.")
	reportUsageCmd.Flags().StringVar(&usageAgentType, "agent-type", "", "Agent type recorded with usage events")

	ClientCmd.AddCommand(cycleCmd)
	ClientCmd.AddCommand(reportUsageCmd)
	ClientCmd.AddCommand(consumeSecretCmd)
	ClientCmd.AddCommand(sendCmd)
	ClientCmd.AddCommand(historyCmd)
	ClientCmd.AddCommand(statusCmd)
	ClientCmd.AddCommand(eventsCmd)
	ClientCmd.AddCommand(deleteSessionCmd)
	ClientCmd.AddCommand(annotateSessionCmd)
	ClientCmd.AddCommand(sendNotificationClientCmd)
	ClientCmd.AddCommand(assetCmd)
	ClientCmd.AddCommand(backupSessionStateCmd)
	ClientCmd.AddCommand(scheduleSessionSuspendCmd)
}

func runConsumeSecret(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	return consumeSecret(ctx, http.DefaultClient, "http://127.0.0.1:9001/one-time-secrets/next", cmd.OutOrStdout())
}

func consumeSecret(ctx context.Context, client *http.Client, endpoint string, output io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("consume one-time secret: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consume one-time secret: local endpoint returned %s", resp.Status)
	}
	var result struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOneTimeSecretResponseBytes)).Decode(&result); err != nil {
		return fmt.Errorf("decode one-time secret: %w", err)
	}
	_, err = fmt.Fprintln(output, result.Value)
	return err
}

const maxOneTimeSecretResponseBytes = 65 << 10

func runReportUsage(cmd *cobra.Command, args []string) error {
	hookInput, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("read stop hook input: %w", err)
	}
	events, err := usagecollector.Collect(hookInput, usageAgentType)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("No usage events found")
		return nil
	}
	c, resolvedSessionID, err := resolveClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := c.ReportUsage(ctx, &entities.UsageEventBatch{SessionID: resolvedSessionID, Events: events})
	if err != nil {
		return err
	}
	fmt.Printf("Usage reported: accepted=%d duplicates=%d\n", result.Accepted, result.Duplicates)
	return nil
}

const (
	cycleEnabledPath = "/tmp/check/CYCLE_ENABLED"
	cycleCountPath   = "/tmp/check/CYCLE_COUNT"
)

// cycleConditionCheckSuffix is appended to every message sent by the cycle command.
// It instructs the agent to check whether the conditions described in the message
// are met, and to delete CYCLE_ENABLED if so (which stops further cycles).
const cycleConditionCheckSuffix = `

---
⚠️ [cycle セッション] このメッセージを受け取ったら、まず上記の達成条件を確認してください。
条件をすべて満たしている場合は、追加の作業は行わずに直ちに以下のコマンドを実行してサイクルを終了させてください:

` + "```" + `bash
rm -f /tmp/check/CYCLE_ENABLED
` + "```" + `

条件を満たしていない場合は、引き続き作業を行ってください。`

// readCycleCount reads the current cycle count from cycleCountPath.
// Returns 0 if the file does not exist.
func readCycleCount() (int, error) {
	data, err := os.ReadFile(cycleCountPath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read cycle count: %w", err)
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &count); err != nil {
		return 0, fmt.Errorf("invalid cycle count in %s: %w", cycleCountPath, err)
	}
	return count, nil
}

// writeCycleCount writes the given count to cycleCountPath.
func writeCycleCount(count int) error {
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		return fmt.Errorf("failed to create /tmp/check: %w", err)
	}
	if err := os.WriteFile(cycleCountPath, []byte(fmt.Sprintf("%d\n", count)), 0o644); err != nil {
		return fmt.Errorf("failed to write cycle count: %w", err)
	}
	return nil
}

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

Optional authentication:
  AGENTAPI_KEY=<api-key>`

// resolveBaseClient creates a client using flags or environment variables.
// Unlike resolveClient, no session-id is required.
func resolveBaseClient() (*client.Client, error) {
	resolvedEndpoint := endpoint
	if resolvedEndpoint == "" {
		envEndpoint, err := client.EndpointFromEnv()
		if err != nil {
			return nil, fmt.Errorf("--endpoint not specified and %w", err)
		}
		resolvedEndpoint = envEndpoint
	}
	apiKey := os.Getenv("AGENTAPI_KEY")
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

func runAssetCreate(cmd *cobra.Command, args []string) {
	html, err := readContentFlag(assetHTML, assetHTMLFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if html == "" {
		fmt.Fprintf(os.Stderr, "Error: provide --html, --html-file, or --html-file -\n")
		os.Exit(1)
	}

	c, err := resolveBaseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	asset, err := c.CreateAsset(context.Background(), &client.CreateAssetRequest{HTML: html})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating asset: %v\n", err)
		os.Exit(1)
	}

	switch assetFormat {
	case "json":
		out, err := json.MarshalIndent(asset, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting response: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	case "url", "":
		fmt.Println(asset.URL)
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported --format %q\n", assetFormat)
		os.Exit(1)
	}
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

const notifyRateLimitFile = "/tmp/notify"
const notifyRateLimitCooldown = 3 * time.Minute

func checkNotifyRateLimit() bool {
	data, err := os.ReadFile(notifyRateLimitFile)
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return err == nil && time.Since(last) < notifyRateLimitCooldown
}

func recordNotifySent() {
	_ = os.WriteFile(notifyRateLimitFile, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func runClientSendNotification(cmd *cobra.Command, args []string) error {
	if clientNotifyTitle == "" {
		return fmt.Errorf("--title is required")
	}
	if clientNotifyBody == "" {
		return fmt.Errorf("--body is required")
	}
	if clientNotifySessionID == "" && clientNotifyUserID == "" {
		return fmt.Errorf("either --notify-session-id or --notify-user-id is required")
	}

	// Client-side rate limiting: skip if a notification was sent recently.
	if checkNotifyRateLimit() {
		fmt.Println("Notification skipped (rate limited)")
		return nil
	}

	c, err := resolveBaseClient()
	if err != nil {
		return fmt.Errorf("failed to resolve client: %w", err)
	}

	req := &client.SendNotificationRequest{
		Title:     clientNotifyTitle,
		Body:      clientNotifyBody,
		SessionID: clientNotifySessionID,
		UserID:    clientNotifyUserID,
	}

	resp, err := c.SendNotification(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	if resp.Success {
		recordNotifySent()
		fmt.Println("Notification sent successfully")
	} else {
		fmt.Fprintf(os.Stderr, "Notification send failed: %s\n", resp.Message)
		os.Exit(1)
	}
	return nil
}
