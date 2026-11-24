package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

type adminOptions struct {
	endpoint string
	apiKey   string
	json     bool
	tail     int
}

type adminRunner struct {
	ID           string `json:"id"`
	ManagerID    string `json:"manager_id"`
	ManagerName  string `json:"manager_name,omitempty"`
	Pool         string `json:"pool,omitempty"`
	FromPool     bool   `json:"from_pool"`
	Status       string `json:"status"`
	SessionID    string `json:"session_id,omitempty"`
	Online       bool   `json:"online"`
	Connectivity struct {
		State  string `json:"state"`
		Source string `json:"source,omitempty"`
	} `json:"connectivity"`
	WorkloadState       string    `json:"workload_state,omitempty"`
	ManagerOnline       *bool     `json:"manager_online,omitempty"`
	ManagerConnectivity string    `json:"manager_connectivity,omitempty"`
	LastSeen            time.Time `json:"last_seen,omitempty"`
}

type adminRunnerList struct {
	Runners []adminRunner `json:"session_runners"`
}

type adminRunnerCleanupResult struct {
	Matched int      `json:"matched"`
	Deleted int      `json:"deleted"`
	Failed  int      `json:"failed"`
	Runners []string `json:"runners"`
}

type adminLogs struct {
	Lines  []string `json:"lines"`
	Source string   `json:"source"`
}

func writeAdminText(w io.Writer, format string, args ...any) error {
	written, err := fmt.Fprintf(w, format, args...)
	if err != nil {
		return err
	}
	if format != "" && written == 0 {
		return io.ErrShortWrite
	}
	return nil
}

// AdminCmd exposes operator-oriented inspection commands.
var AdminCmd = newAdminCommand()

func newAdminCommand() *cobra.Command {
	opts := &adminOptions{}
	root := &cobra.Command{
		Use:   "admin",
		Short: "Inspect runners, sessions, and operational logs",
		Long: `Administrative inspection commands for ccplant.

The endpoint and authentication token are read from flags, environment variables,
or ~/.ccplant/config.yaml. The API token must have admin permission.`,
	}
	root.PersistentFlags().StringVarP(&opts.endpoint, "endpoint", "e", "", "AgentAPI proxy endpoint")
	root.PersistentFlags().StringVar(&opts.apiKey, "api-key", "", "admin API token")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "print JSON output")

	runners := &cobra.Command{Use: "runners", Short: "List and clean up runners", Args: cobra.NoArgs}
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
		if err := writeAdminText(w, "RUNNER ID\tLIFECYCLE\tRUNNER\tWORKLOAD\tMANAGER\tSESSION ID\tMANAGER NAME\tPOOL\tLAST SEEN\n"); err != nil {
			return err
		}
		for _, item := range items {
			manager := item.ManagerName
			if manager == "" {
				manager = item.ManagerID
			}
			if err := writeAdminText(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Status, adminRunnerConnectivity(item), emptyDash(item.WorkloadState), adminRunnerManagerConnectivity(item), emptyDash(item.SessionID), manager, emptyDash(item.Pool), formatAdminTime(item.LastSeen)); err != nil {
				return err
			}
		}
		return w.Flush()
	}
	runners.AddCommand(newAdminDeleteRunnerCommand(opts), newAdminCleanupOfflineRunnersCommand(opts))

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
		if err := writeAdminText(w, "SESSION ID\t%s\nSTATUS\t%s\nUSER ID\t%s\nSTARTED AT\t%s\n", item.SessionID, item.Status, item.UserID, formatAdminTime(item.StartedAt)); err != nil {
			return err
		}
		if item.AllocatedSessionID != "" {
			if err := writeAdminText(w, "ALLOCATED SESSION ID\t%s\n", item.AllocatedSessionID); err != nil {
				return err
			}
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

func newAdminDeleteRunnerCommand(opts *adminOptions) *cobra.Command {
	var managerID string
	var confirm bool
	var force bool
	command := &cobra.Command{
		Use:   "delete <runner-id>",
		Short: "Delete one runner",
		Long: `Delete one runner and its associated allocation and route metadata.

The manager is resolved from the runner inventory. Use --manager-id when the
runner ID is ambiguous. An online manager also deletes the workload and related
resources. For an offline manager, --force removes only parent-side metadata and
may leave resources in the manager cluster.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return errors.New("refusing to delete runner without --confirm")
			}
			api, err := opts.api()
			if err != nil {
				return err
			}
			items, err := api.runners(cmd.Context())
			if err != nil {
				return err
			}
			runner, err := findRunner(items, func(item adminRunner) bool {
				return item.ID == args[0] && (managerID == "" || item.ManagerID == managerID)
			}, "runner", args[0])
			if err != nil {
				return err
			}
			managerState := adminRunnerManagerConnectivity(*runner)
			if managerState == "unknown" {
				return errors.New("runner's session manager connectivity is unknown; refusing deletion until it can be determined")
			}
			if managerState == "offline" && !force {
				return errors.New("runner's session manager is offline; pass --force to remove parent-side metadata only")
			}
			if err := api.deleteRunner(cmd.Context(), *runner, force); err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"deleted": true, "runner": runner.ID, "manager_id": runner.ManagerID, "force": force})
			}
			return writeAdminText(cmd.OutOrStdout(), "Deleted runner %s (manager=%s).\n", runner.ID, runner.ManagerID)
		},
	}
	flags := command.Flags()
	flags.StringVar(&managerID, "manager-id", "", "session manager ID (required when the runner ID is ambiguous)")
	flags.BoolVar(&confirm, "confirm", false, "confirm runner deletion")
	flags.BoolVar(&force, "force", false, "remove parent-side metadata when the session manager is offline")
	return command
}

func newAdminCleanupOfflineRunnersCommand(opts *adminOptions) *cobra.Command {
	var confirm bool
	command := &cobra.Command{
		Use:   "cleanup-offline",
		Short: "Clean up all runners whose session manager is offline",
		Long: `Clean up parent-side metadata for every runner whose session manager is offline.

The command previews matching runners by default. Pass --confirm to remove their
runner records, allocations, and route metadata. Workloads and related resources
may remain in the manager cluster, so only run this after confirming that the
offline managers will not recover.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := opts.api()
			if err != nil {
				return err
			}
			items, err := api.runners(cmd.Context())
			if err != nil {
				return err
			}
			offline := make([]adminRunner, 0)
			for _, item := range items {
				if adminRunnerManagerOffline(item) {
					offline = append(offline, item)
				}
			}

			result := adminRunnerCleanupResult{Matched: len(offline), Runners: make([]string, 0, len(offline))}
			for _, item := range offline {
				result.Runners = append(result.Runners, item.ManagerID+":"+item.ID)
			}
			if !confirm {
				if opts.json {
					return writeJSON(cmd.OutOrStdout(), result)
				}
				if len(offline) == 0 {
					return writeAdminText(cmd.OutOrStdout(), "No offline runners found.\n")
				}
				if err := writeAdminText(cmd.OutOrStdout(), "Found %d offline runner(s):\n", len(offline)); err != nil {
					return err
				}
				for _, item := range offline {
					if err := writeAdminText(cmd.OutOrStdout(), "  %s (manager=%s, session=%s)\n", item.ID, item.ManagerID, emptyDash(item.SessionID)); err != nil {
						return err
					}
				}
				return writeAdminText(cmd.OutOrStdout(), "Preview only. Re-run with --confirm to clean them up.\n")
			}

			var failures []string
			for _, item := range offline {
				if err := api.deleteRunner(cmd.Context(), item, true); err != nil {
					result.Failed++
					failures = append(failures, fmt.Sprintf("%s (manager=%s): %v", item.ID, item.ManagerID, err))
					if !opts.json {
						if writeErr := writeAdminText(cmd.OutOrStdout(), "[FAILED] %s (manager=%s): %v\n", item.ID, item.ManagerID, err); writeErr != nil {
							return writeErr
						}
					}
					continue
				}
				result.Deleted++
				if !opts.json {
					if err := writeAdminText(cmd.OutOrStdout(), "[DELETED] %s (manager=%s)\n", item.ID, item.ManagerID); err != nil {
						return err
					}
				}
			}
			if opts.json {
				if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
			} else {
				if err := writeAdminText(cmd.OutOrStdout(), "Cleaned up %d of %d offline runner(s).\n", result.Deleted, result.Matched); err != nil {
					return err
				}
			}
			if len(failures) > 0 {
				return fmt.Errorf("failed to clean up %d offline runner(s): %s", len(failures), strings.Join(failures, "; "))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&confirm, "confirm", false, "delete all matching offline runners")
	return command
}

func adminRunnerManagerOnline(item adminRunner) bool {
	if item.ManagerConnectivity != "" {
		return item.ManagerConnectivity == "online"
	}
	if item.ManagerOnline != nil {
		return *item.ManagerOnline
	}
	// Older APIs exposed only online, where manager reachability and runner
	// reachability were conflated. Preserve compatibility during a rolling update.
	return item.Online
}

func adminRunnerConnectivity(item adminRunner) string {
	if item.Connectivity.State != "" {
		return item.Connectivity.State
	}
	if item.Online {
		return "online"
	}
	return "unknown"
}

func adminRunnerManagerConnectivity(item adminRunner) string {
	if item.ManagerConnectivity != "" {
		return item.ManagerConnectivity
	}
	if adminRunnerManagerOnline(item) {
		return "online"
	}
	return "offline"
}

func adminRunnerManagerOffline(item adminRunner) bool {
	if item.ManagerConnectivity != "" {
		return item.ManagerConnectivity == "offline"
	}
	return !adminRunnerManagerOnline(item)
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
			if err := writeAdminText(cmd.OutOrStdout(), "%s\n", line); err != nil {
				return err
			}
		}
		return nil
	}}
}

type adminAPI struct {
	baseURL string
	apiKey  string
	client  *http.Client
}
