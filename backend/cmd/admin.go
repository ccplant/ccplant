package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

type adminOptions struct {
	endpoint string
	apiKey   string
	json     bool
	tail     int
}

type adminRunner struct {
	ID          string    `json:"id"`
	ManagerID   string    `json:"manager_id"`
	ManagerName string    `json:"manager_name,omitempty"`
	Pool        string    `json:"pool,omitempty"`
	FromPool    bool      `json:"from_pool"`
	Status      string    `json:"status"`
	SessionID   string    `json:"session_id,omitempty"`
	Online      bool      `json:"online"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

type adminRunnerList struct {
	Runners []adminRunner `json:"session_runners"`
}

type adminLogs struct {
	Lines  []string `json:"lines"`
	Source string   `json:"source"`
}

// AdminCmd exposes operator-oriented inspection commands.
var AdminCmd = newAdminCommand()

func newAdminCommand() *cobra.Command {
	opts := &adminOptions{}
	root := &cobra.Command{
		Use:   "admin",
		Short: "Inspect runners, sessions, and operational logs",
		Long: `Administrative inspection commands for ccplant.

The endpoint is read from --endpoint or AGENTAPI_PROXY_ENDPOINT. Authentication
uses --api-key or AGENTAPI_KEY. The API key must have admin permission.`,
	}
	root.PersistentFlags().StringVarP(&opts.endpoint, "endpoint", "e", "", "AgentAPI proxy endpoint (defaults to AGENTAPI_PROXY_ENDPOINT)")
	root.PersistentFlags().StringVar(&opts.apiKey, "api-key", "", "admin API key (defaults to AGENTAPI_KEY)")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "print JSON output")

	runners := &cobra.Command{Use: "runners", Short: "List runners and their current state", Args: cobra.NoArgs}
	runners.RunE = func(cmd *cobra.Command, _ []string) error {
		api, err := opts.api()
		if err != nil {
			return err
		}
		items, err := api.runners(cmd.Context())
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), adminRunnerList{Runners: items})
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "RUNNER ID\tSTATUS\tONLINE\tSESSION ID\tMANAGER\tPOOL\tLAST SEEN")
		for _, item := range items {
			manager := item.ManagerName
			if manager == "" {
				manager = item.ManagerID
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Online, emptyDash(item.SessionID), manager, emptyDash(item.Pool), formatAdminTime(item.LastSeen))
		}
		return w.Flush()
	}

	session := &cobra.Command{Use: "session <session-id>", Short: "Show a session's state", Args: cobra.ExactArgs(1)}
	session.RunE = func(cmd *cobra.Command, args []string) error {
		api, err := opts.api()
		if err != nil {
			return err
		}
		item, err := api.session(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), item)
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintf(w, "SESSION ID\t%s\nSTATUS\t%s\nUSER ID\t%s\nSTARTED AT\t%s\n", item.SessionID, item.Status, item.UserID, formatAdminTime(item.StartedAt))
		if item.AllocatedSessionID != "" {
			_, _ = fmt.Fprintf(w, "ALLOCATED SESSION ID\t%s\n", item.AllocatedSessionID)
		}
		return w.Flush()
	}

	logs := &cobra.Command{Use: "logs", Short: "View runner, session manager, or session logs"}
	logs.PersistentFlags().IntVarP(&opts.tail, "tail", "n", 200, "number of log lines (1-5000)")
	logs.PersistentPreRunE = func(*cobra.Command, []string) error {
		if opts.tail < 1 || opts.tail > 5000 {
			return errors.New("--tail must be between 1 and 5000")
		}
		return nil
	}
	logs.AddCommand(newAdminLogsCommand(opts, "runner <runner-id>", "View logs for a runner", func(ctx context.Context, api *adminAPI, id string) (*adminLogs, error) {
		items, err := api.runners(ctx)
		if err != nil {
			return nil, err
		}
		item, err := findRunner(items, func(item adminRunner) bool { return item.ID == id }, "runner", id)
		if err != nil {
			return nil, err
		}
		return api.logs(ctx, "/admin/session-runners/"+url.PathEscape(item.ID)+"/logs", url.Values{"manager_id": {item.ManagerID}, "session_id": {item.SessionID}, "scope": {"system"}, "tail": {strconv.Itoa(opts.tail)}})
	}))
	logs.AddCommand(newAdminLogsCommand(opts, "session-manager <manager-id>", "View session manager logs", func(ctx context.Context, api *adminAPI, id string) (*adminLogs, error) {
		return api.logs(ctx, "/admin/session-managers/"+url.PathEscape(id)+"/logs", url.Values{"tail": {strconv.Itoa(opts.tail)}})
	}))
	logs.AddCommand(newAdminLogsCommand(opts, "session <session-id>", "View logs for a session", func(ctx context.Context, api *adminAPI, id string) (*adminLogs, error) {
		items, err := api.runners(ctx)
		if err != nil {
			return nil, err
		}
		item, err := findRunner(items, func(item adminRunner) bool { return item.SessionID == id }, "session", id)
		if err != nil {
			return nil, err
		}
		return api.logs(ctx, "/admin/session-runners/"+url.PathEscape(item.ID)+"/logs", url.Values{"manager_id": {item.ManagerID}, "session_id": {id}, "scope": {"system"}, "tail": {strconv.Itoa(opts.tail)}})
	}))

	root.AddCommand(runners, session, logs)
	return root
}

func newAdminLogsCommand(opts *adminOptions, use, short string, get func(context.Context, *adminAPI, string) (*adminLogs, error)) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		api, err := opts.api()
		if err != nil {
			return err
		}
		result, err := get(cmd.Context(), api, args[0])
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), result)
		}
		for _, line := range result.Lines {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return nil
	}}
}

type adminAPI struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (opts *adminOptions) api() (*adminAPI, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(opts.endpoint), "/")
	if endpoint == "" {
		var err error
		endpoint, err = client.EndpointFromEnv()
		if err != nil {
			return nil, fmt.Errorf("--endpoint not specified and %w", err)
		}
		endpoint = strings.TrimRight(endpoint, "/")
	}
	apiKey := opts.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("AGENTAPI_KEY")
	}
	return &adminAPI{baseURL: endpoint, apiKey: apiKey, client: http.DefaultClient}, nil
}

func (a *adminAPI) get(ctx context.Context, path string, query url.Values, out any) error {
	u, err := url.Parse(a.baseURL + path)
	if err != nil {
		return err
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fmt.Errorf("request %s failed: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return nil
}

func (a *adminAPI) runners(ctx context.Context) ([]adminRunner, error) {
	var result adminRunnerList
	if err := a.get(ctx, "/admin/session-runners", url.Values{"scope": {"system"}}, &result); err != nil {
		return nil, err
	}
	return result.Runners, nil
}

func (a *adminAPI) session(ctx context.Context, id string) (*client.SessionInfo, error) {
	var result client.SearchResponse
	if err := a.get(ctx, "/search", nil, &result); err != nil {
		return nil, err
	}
	for i := range result.Sessions {
		if result.Sessions[i].SessionID == id {
			return &result.Sessions[i], nil
		}
	}
	return nil, fmt.Errorf("session %q not found", id)
}

func (a *adminAPI) logs(ctx context.Context, path string, query url.Values) (*adminLogs, error) {
	var result adminLogs
	if err := a.get(ctx, path, query, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func findRunner(items []adminRunner, matches func(adminRunner) bool, kind, id string) (*adminRunner, error) {
	var found *adminRunner
	for i := range items {
		if !matches(items[i]) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%s %q matches multiple runners", kind, id)
		}
		found = &items[i]
	}
	if found == nil {
		return nil, fmt.Errorf("%s %q not found", kind, id)
	}
	return found, nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatAdminTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format(time.RFC3339)
}
