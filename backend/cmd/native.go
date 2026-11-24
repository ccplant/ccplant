package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const nativeDefaultInstance = "default"

var nativeInstanceNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

var NativeCmd = &cobra.Command{
	Use:   "native",
	Short: "Install and manage a native External Session Manager",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// `native list` enumerates every instance, so it does not select one.
		if cmd.Name() == "list" {
			return nil
		}
		return validateNativeInstanceName(nativeManageOpts.instance)
	},
}

type nativeManageOptions struct {
	upstream, name, listen, apiKeyEnv, apiKeyFile, configPath, instance                                  string
	scope, teamID, drainTimeout, registrationToken                                                       string
	labels                                                                                               []string
	environment                                                                                          []string
	automaticAssignmentEnabled, apiKeyStdin, force, drain, keepRegistration, keepData, filesystemSandbox bool
	inheritRuntimeProfile                                                                                bool
	logsFollow, logsDaemon                                                                               bool
	jsonOutput                                                                                           bool
	logsTail                                                                                             int
}

var nativeManageOpts nativeManageOptions

type nativeRegistrationResponse struct {
	ID              string            `json:"id"`
	InstanceID      string            `json:"instance_id"`
	Name            string            `json:"name"`
	Scope           string            `json:"scope"`
	OwnerID         string            `json:"owner_id"`
	ConnectionToken string            `json:"connection_token"`
	Labels          map[string]string `json:"labels"`
	Created         bool              `json:"created"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at"`
}

func init() {
	p := NativeCmd.PersistentFlags()
	p.StringVar(&nativeManageOpts.configPath, "config", "", "native daemon config path")
	p.StringVar(&nativeManageOpts.instance, "instance", "", "named native ESM instance; omit to select the default instance")
	p.StringVar(&nativeManageOpts.apiKeyEnv, "api-key-env", "AGENTAPI_KEY", "environment variable containing the API key for authenticated lifecycle operations")
	p.StringVar(&nativeManageOpts.apiKeyFile, "api-key-file", "", "file containing the API key for authenticated lifecycle operations")
	p.BoolVar(&nativeManageOpts.apiKeyStdin, "api-key-stdin", false, "read the API key for authenticated lifecycle operations from stdin")

	install := &cobra.Command{Use: "install", Short: "Register and install the native ESM daemon", RunE: runNativeInstall}
	f := install.Flags()
	f.StringVar(&nativeManageOpts.upstream, "upstream", "", "parent agentapi-proxy URL")
	f.StringVar(&nativeManageOpts.name, "name", "", "human-readable manager name")
	f.StringVar(&nativeManageOpts.listen, "listen", ":8080", "native ESM listen address")
	f.StringVar(&nativeManageOpts.scope, "scope", "user", "registration scope: user or team")
	f.StringVar(&nativeManageOpts.teamID, "team-id", "", "team ID when --scope=team")
	f.StringVar(&nativeManageOpts.registrationToken, "registration-token", "", "one-time registration token issued by the parent proxy")
	f.StringSliceVar(&nativeManageOpts.labels, "label", nil, "allocator label in key=value form")
	f.StringArrayVar(&nativeManageOpts.environment, "manager-env", nil, "native manager environment variable in KEY=VALUE form (repeatable)")
	f.BoolVar(&nativeManageOpts.automaticAssignmentEnabled, "automatic-assignment-enabled", false, "allow automatic assignment to this external session manager")
	f.BoolVar(&nativeManageOpts.force, "force", false, "install even if the existing state directory contains sessions")
	f.BoolVar(&nativeManageOpts.filesystemSandbox, "filesystem-sandbox", false, "sandbox native session filesystem access on macOS")
	f.BoolVar(&nativeManageOpts.inheritRuntimeProfile, "inherit-runtime-profile", false, "apply runtime profile received from the parent proxy")

	status := &cobra.Command{Use: "status", Short: "Show daemon and connection status", RunE: runNativeStatus}
	status.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	doctor := &cobra.Command{Use: "doctor", Short: "Validate daemon configuration and connectivity", RunE: runNativeDoctor}
	doctor.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	restart := &cobra.Command{Use: "restart", Short: "Restart the native ESM daemon", RunE: runNativeRestart}
	update := &cobra.Command{Use: "update", Short: "Replace the managed daemon binary with this executable and restart", RunE: runNativeUpdate}
	rotate := &cobra.Command{Use: "rotate-token", Short: "Rotate the ESM connection token and restart", RunE: runNativeRotateToken}
	uninstall := &cobra.Command{Use: "uninstall", Short: "Stop and remove the native ESM daemon", RunE: runNativeUninstall}
	uninstall.Flags().BoolVar(&nativeManageOpts.force, "force", false, "terminate active sessions")
	uninstall.Flags().BoolVar(&nativeManageOpts.drain, "drain", false, "wait for active sessions to finish")
	uninstall.Flags().StringVar(&nativeManageOpts.drainTimeout, "drain-timeout", "30m", "maximum time to wait with --drain")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepRegistration, "keep-registration", false, "keep the parent registration")
	uninstall.Flags().BoolVar(&nativeManageOpts.keepData, "keep-data", false, "keep daemon state and configuration")
	listCmd := &cobra.Command{Use: "list", Short: "List installed native ESM instances on this host", Args: cobra.NoArgs, RunE: runNativeList}
	listCmd.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	sessionList := &cobra.Command{Use: "session-list", Aliases: []string{"sessions"}, Short: "List native sessions on this host", Args: cobra.NoArgs, RunE: runNativeSessionList}
	sessionList.Flags().BoolVar(&nativeManageOpts.jsonOutput, "json", false, "output machine-readable JSON")
	sessionDelete := &cobra.Command{Use: "session-delete <session-id>", Short: "Delete a native session on this host", Args: cobra.ExactArgs(1), RunE: runNativeSessionDelete}
	logs := &cobra.Command{Use: "logs [session-id]", Short: "Show native daemon or session logs", Args: cobra.MaximumNArgs(1), RunE: runNativeLogs}
	logs.Flags().BoolVarP(&nativeManageOpts.logsFollow, "follow", "f", false, "follow log output")
	logs.Flags().IntVarP(&nativeManageOpts.logsTail, "tail", "n", 100, "number of lines to show")
	logs.Flags().BoolVar(&nativeManageOpts.logsDaemon, "daemon", false, "show the native daemon log")
	NativeCmd.AddCommand(install, status, doctor, restart, update, rotate, uninstall, listCmd, sessionList, sessionDelete, logs)
}

// resolveNativeInstance validates and normalizes the selected instance name,
// returning nativeDefaultInstance when --instance is omitted.
func resolveNativeInstance() (string, error) {
	if err := validateNativeInstanceName(nativeManageOpts.instance); err != nil {
		return "", err
	}
	if nativeManageOpts.instance == "" {
		return nativeDefaultInstance, nil
	}
	return nativeManageOpts.instance, nil
}

func validateNativeInstanceName(name string) error {
	if name == "" {
		return nil
	}
	if name == nativeDefaultInstance {
		return nil
	}
	if !nativeInstanceNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid --instance %q; use 1-32 lowercase letters, digits, and hyphens (hyphens only in the middle)", name)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func runNativeInstall(command *cobra.Command, _ []string) error {
	if nativeManageOpts.upstream == "" {
		return errors.New("--upstream is required")
	}
	if nativeManageOpts.filesystemSandbox && runtime.GOOS != "darwin" {
		return errors.New("--filesystem-sandbox is only supported on macOS")
	}
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	if nativeManageOpts.configPath != "" && instance != nativeDefaultInstance {
		return errors.New("--config cannot be combined with a non-default --instance; the instance selects its own isolated configuration path")
	}
	if instance != nativeDefaultInstance && !command.Flags().Changed("listen") {
		return errors.New("--listen is required for non-default instances; specify a distinct port so it does not collide with the default :8080")
	}
	hostname, _ := os.Hostname()
	if nativeManageOpts.name == "" {
		nativeManageOpts.name = hostname
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	active, _ := filepath.Glob(filepath.Join(paths.state, "sessions", "*"))
	if len(active) > 0 && !nativeManageOpts.force {
		return fmt.Errorf("refusing to replace daemon with %d session(s) in state; drain them first or use --force", len(active))
	}
	existing, _ := readNativeConfig(paths.config)
	instanceID := existing.InstanceID
	if instanceID == "" {
		instanceID = stableNativeInstanceID(hostname, instance)
	}
	labels := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "hostname": hostname, "native_instance": instance}
	for _, raw := range nativeManageOpts.labels {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return fmt.Errorf("invalid --label %q; expected key=value", raw)
		}
		key := strings.TrimSpace(parts[0])
		if key == "native_instance" {
			return fmt.Errorf("invalid --label %q; the native_instance label is managed automatically by --instance", raw)
		}
		labels[key] = parts[1]
	}
	environment := make(map[string]string, len(existing.ManagerEnvironment)+len(nativeManageOpts.environment))
	for key, value := range existing.ManagerEnvironment {
		environment[key] = value
	}
	for _, raw := range nativeManageOpts.environment {
		key, value, parseErr := parseNativeManagerEnvironment(raw)
		if parseErr != nil {
			return parseErr
		}
		environment[key] = value
	}
	if nativeManageOpts.registrationToken == "" {
		return errors.New("--registration-token is required")
	}
	payload := map[string]interface{}{
		"instance_id": instanceID, "name": nativeManageOpts.name,
		"labels": labels, "automatic_assignment_enabled": nativeManageOpts.automaticAssignmentEnabled,
		"version": nativeBuildVersion(), "registration_token": nativeManageOpts.registrationToken,
	}
	registration, err := enrollNativeManager(nativeManageOpts.upstream, payload)
	if err != nil {
		return err
	}
	token := registration.ConnectionToken
	if token == "" {
		token = existing.ConnectionToken
	}
	if token == "" {
		return errors.New("registration did not return a connection token; run native rotate-token")
	}
	cfg := nativeDaemonConfig{Listen: nativeManageOpts.listen, UpstreamURL: strings.TrimRight(nativeManageOpts.upstream, "/"),
		ConnectionToken: token, StateDir: paths.state,
		BinaryPath: paths.binary, ManagerID: registration.ID, Name: firstNonEmpty(registration.Name, nativeManageOpts.name), InstanceID: instanceID, Scope: firstNonEmpty(registration.Scope, nativeManageOpts.scope), TeamID: firstNonEmpty(registration.OwnerID, nativeManageOpts.teamID),
		Labels: labels, ManagerEnvironment: environment, Version: nativeBuildVersion(),
		FilesystemSandbox:     nativeFilesystemSandboxConfig{Enabled: nativeManageOpts.filesystemSandbox},
		InheritRuntimeProfile: nativeManageOpts.inheritRuntimeProfile}
	if err := installNativeService(paths, cfg); err != nil {
		return err
	}
	if err := waitNativeHealth(cfg.Listen, 30*time.Second); err != nil {
		return err
	}
	if err := sendNativeHeartbeat(cfg); err != nil {
		return fmt.Errorf("daemon installed but parent heartbeat failed: %w", err)
	}
	fmt.Printf("Native ESM installed\nInstance: %s\nManager ID: %s\nService: %s\nLabels: %s\n", instance, registration.ID, nativeServiceName(instance), formatLabels(labels))
	return nil
}
