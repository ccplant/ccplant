package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
	"github.com/takutakahashi/agentapi-proxy/pkg/usagecollector"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
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
// to environment variables and the user CLI config. Session ID can be provided
// only by flag or AGENTAPI_SESSION_ID.
// Returns the client and the resolved session ID.
func resolveClient() (*client.Client, string, error) {
	resolvedSessionID := sessionID
	resolvedEndpoint, apiKey, err := resolveCLIConnection(context.Background(), endpoint, "")
	if err != nil {
		return nil, "", err
	}

	if resolvedSessionID == "" {
		resolvedSessionID = os.Getenv("AGENTAPI_SESSION_ID")
		if resolvedSessionID == "" {
			return nil, "", fmt.Errorf("--session-id not specified and AGENTAPI_SESSION_ID is not set")
		}
	}

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
  ccplant client cycle --max-count 10

  ccplant client cycle --session-id my-session`,
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
  ccplant client delete-session

  # Delete current session without confirmation
  ccplant client delete-session --confirm`,
	Run: runDeleteSession,
}

var annotateSessionCmd = &cobra.Command{
	Use:   "annotate-session",
	Short: "Update current session info",
	Long: `Update the current session's user-managed info.

The supported fields are PR URL, issue URL, description, and running task.
At least one flag must be specified. Use an explicit empty string to clear a field.

Examples:
  ccplant client annotate-session \
    --endpoint http://proxy:8080 \
    --session-id my-session \
    --pr-url https://github.com/owner/repo/pull/123 \
    --issue-url https://github.com/owner/repo/issues/456 \
    --description "Session annotation support" \
    --running-task "Implement session annotations"

  # Clear the running task
  ccplant client annotate-session --running-task ""`,
	Run: runAnnotateSession,
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
	ClientCmd.AddCommand(backupSessionStateCmd)
	ClientCmd.AddCommand(scheduleSessionSuspendCmd)
}

func runConsumeSecret(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	if socketPath := strings.TrimSpace(os.Getenv("PROVISIONER_LOCAL_SOCKET")); socketPath != "" {
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}}
		return consumeSecret(ctx, &http.Client{Transport: transport}, "http://unix/one-time-secrets/next", cmd.OutOrStdout())
	}
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
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
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
