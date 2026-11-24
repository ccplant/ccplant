package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type nativeStatusOutput struct {
	Instance              string            `json:"instance"`
	Service               string            `json:"service"`
	ManagerID             string            `json:"manager_id"`
	Name                  string            `json:"name"`
	Upstream              string            `json:"upstream"`
	Listen                string            `json:"listen"`
	Scope                 string            `json:"scope"`
	TeamID                string            `json:"team_id"`
	Labels                map[string]string `json:"labels"`
	Version               string            `json:"version"`
	FilesystemSandbox     bool              `json:"filesystem_sandbox"`
	InheritRuntimeProfile bool              `json:"inherit_runtime_profile"`
	ActiveSessions        int               `json:"active_sessions"`
	Health                string            `json:"health"`
	State                 string            `json:"state"`
}

func runNativeStatus(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	service := "stopped"
	if nativeServiceRunning(instance) {
		service = "running"
	}
	health := "unreachable"
	if nativeHealth(cfg.Listen) == nil {
		health = "ok"
	}
	sessions, err := readNativeSessionList(cfg.StateDir)
	if err != nil {
		return fmt.Errorf("read native sessions: %w", err)
	}
	activeSessions := 0
	for _, session := range sessions {
		if nativeCLIStatusActive(session.Status) && processExists(session.PID) {
			activeSessions++
		}
	}
	status := nativeStatusOutput{Instance: instance, Service: service, ManagerID: cfg.ManagerID, Name: cfg.Name, Upstream: cfg.UpstreamURL,
		Listen: cfg.Listen, Scope: cfg.Scope, TeamID: cfg.TeamID,
		Labels: cfg.Labels, Version: cfg.Version, FilesystemSandbox: cfg.FilesystemSandbox.Enabled,
		InheritRuntimeProfile: cfg.InheritRuntimeProfile,
		ActiveSessions:        activeSessions, Health: health, State: cfg.StateDir}
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(status)
	}
	output := fmt.Sprintf("Instance: %s\nService: %s\nManager name: %s\nManager ID: %s\nUpstream: %s\nLabels: %s\nVersion: %s\nFilesystem sandbox: %t\nActive sessions: %d\nHealth: %s\nState: %s\n", instance, service, firstNonEmpty(cfg.Name, "-"), cfg.ManagerID, cfg.UpstreamURL, formatLabels(cfg.Labels), cfg.Version, cfg.FilesystemSandbox.Enabled, activeSessions, health, cfg.StateDir)
	return writeNativeOutput(command.OutOrStdout(), output)
}

func nativeCLIStatusActive(status string) bool {
	switch strings.ToLower(status) {
	case "error", "failed", "exited", "stopped", "terminated":
		return false
	default:
		return true
	}
}

func processExists(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

type nativeSessionListEntry struct {
	ID            string            `json:"id"`
	PID           int               `json:"pid"`
	StartedAt     time.Time         `json:"started_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	LastMessageAt time.Time         `json:"last_message_at"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message,omitempty"`
	Description   string            `json:"description,omitempty"`
	UserID        string            `json:"user_id,omitempty"`
	Scope         string            `json:"scope,omitempty"`
	TeamID        string            `json:"team_id,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	LogPath       string            `json:"log_path"`
}

func readNativeSessionList(stateDir string) ([]nativeSessionListEntry, error) {
	sessionDirs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*"))
	if err != nil {
		return nil, err
	}
	entries := make([]nativeSessionListEntry, 0, len(sessionDirs))
	for _, sessionDir := range sessionDirs {
		data, readErr := os.ReadFile(filepath.Join(sessionDir, "runtime", "state.json"))
		if readErr != nil {
			continue
		}
		var entry nativeSessionListEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("read session state %s: %w", filepath.Base(sessionDir), err)
		}
		var metadata struct {
			Request *struct {
				UserID string            `json:"user_id"`
				Scope  string            `json:"scope"`
				TeamID string            `json:"team_id"`
				Tags   map[string]string `json:"tags"`
			} `json:"request"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, fmt.Errorf("read session metadata %s: %w", filepath.Base(sessionDir), err)
		}
		if metadata.Request != nil {
			entry.UserID, entry.Scope, entry.TeamID, entry.Tags = metadata.Request.UserID, metadata.Request.Scope, metadata.Request.TeamID, metadata.Request.Tags
		}
		if entry.ID == "" {
			entry.ID = filepath.Base(sessionDir)
		}
		entry.LogPath = filepath.Join(sessionDir, "runtime", "provisioner.log")
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].StartedAt.Equal(entries[j].StartedAt) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].StartedAt.After(entries[j].StartedAt)
	})
	return entries, nil
}

func runNativeSessionList(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	entries, err := readNativeSessionList(cfg.StateDir)
	if err != nil {
		return err
	}
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}
	w := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	if err := writeNativeOutput(w, "SESSION ID\tSTATUS\tPID\tSTARTED\tLOG\n"); err != nil {
		return err
	}
	for _, entry := range entries {
		started := "-"
		if !entry.StartedAt.IsZero() {
			started = entry.StartedAt.Local().Format(time.RFC3339)
		}
		line := fmt.Sprintf("%s\t%s\t%d\t%s\t%s\n", entry.ID, entry.Status, entry.PID, started, entry.LogPath)
		if err := writeNativeOutput(w, line); err != nil {
			return err
		}
	}
	return w.Flush()
}

func runNativeSessionDelete(command *cobra.Command, args []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return fmt.Errorf("read native daemon config: %w", err)
	}
	sessionID := strings.TrimSpace(args[0])
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	base, err := nativeSessionManagerLocalURL(cfg.Listen)
	if err != nil {
		return err
	}
	path := "/api/v1/sessions/" + url.PathEscape(sessionID)
	req, err := http.NewRequestWithContext(command.Context(), http.MethodDelete, base+path, nil)
	if err != nil {
		return err
	}
	ts := hmacutil.NowTimestamp()
	req.Header.Set(hmacutil.TimestampHeader, ts)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(cfg.ConnectionToken), hmacutil.BuildMessage(http.MethodDelete, path, ts, nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete native session: %w", err)
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return responseError(resp)
	}
	return writeNativeOutput(command.OutOrStdout(), fmt.Sprintf("Deleted native session %s\n", sessionID))
}

func nativeLogPath(paths nativeInstallPaths, cfg nativeDaemonConfig, sessionID string, daemon bool) (string, error) {
	if daemon {
		if sessionID != "" {
			return "", errors.New("a session ID cannot be used with --daemon")
		}
		return filepath.Join(paths.logDir, "native.log"), nil
	}
	if sessionID == "" {
		return "", errors.New("session ID is required (or use --daemon)")
	}
	if sessionID == "." || filepath.Base(sessionID) != sessionID {
		return "", errors.New("invalid session ID")
	}
	return filepath.Join(cfg.StateDir, "sessions", sessionID, "runtime", "provisioner.log"), nil
}

func runNativeLogs(command *cobra.Command, args []string) error {
	if nativeManageOpts.logsTail < 0 {
		return errors.New("--tail must be zero or greater")
	}
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return err
	}
	sessionID := ""
	if len(args) == 1 {
		sessionID = args[0]
	}
	logPath, err := nativeLogPath(paths, cfg, sessionID, nativeManageOpts.logsDaemon)
	if err != nil {
		return err
	}
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	tailArgs := []string{"-n", strconv.Itoa(nativeManageOpts.logsTail)}
	if nativeManageOpts.logsFollow {
		tailArgs = append(tailArgs, "-F")
	}
	tailArgs = append(tailArgs, logPath)
	tail := exec.CommandContext(command.Context(), "tail", tailArgs...)
	tail.Stdout = command.OutOrStdout()
	tail.Stderr = command.ErrOrStderr()
	return tail.Run()
}
