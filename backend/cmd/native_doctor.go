package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

type nativeDoctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type nativeDoctorOutput struct {
	OK     bool                `json:"ok"`
	Checks []nativeDoctorCheck `json:"checks"`
}

type nativeDoctorRecorder struct {
	result   nativeDoctorOutput
	firstErr error
}

func newNativeDoctorRecorder() *nativeDoctorRecorder {
	return &nativeDoctorRecorder{result: nativeDoctorOutput{OK: true, Checks: make([]nativeDoctorCheck, 0, 5)}}
}

func (r *nativeDoctorRecorder) record(id, success string, err error) {
	entry := nativeDoctorCheck{ID: id, Status: "ok", Message: success}
	if err != nil {
		entry.Status = "error"
		entry.Message = err.Error()
		r.result.OK = false
		if r.firstErr == nil {
			r.firstErr = err
		}
	}
	r.result.Checks = append(r.result.Checks, entry)
}

func runNativeDoctor(command *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	recorder := newNativeDoctorRecorder()
	recordNativePermissionChecks(recorder, paths)
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		recorder.record("config_load", "configuration loaded", fmt.Errorf("configuration: %w", err))
		if nativeManageOpts.jsonOutput {
			return json.NewEncoder(command.OutOrStdout()).Encode(recorder.result)
		}
		return err
	}
	recorder.record("config_load", "configuration loaded", nil)
	sandboxMessage, sandboxErr := nativeSandboxCheck(cfg)
	recorder.record("filesystem_sandbox", sandboxMessage, sandboxErr)
	localErr := nativeHealth(cfg.Listen)
	if localErr != nil {
		localErr = fmt.Errorf("local health: %w", localErr)
	}
	recorder.record("local_health", "local daemon is healthy", localErr)
	heartbeatErr := sendNativeHeartbeat(cfg)
	if heartbeatErr != nil {
		heartbeatErr = fmt.Errorf("parent heartbeat: %w", heartbeatErr)
	}
	recorder.record("parent_heartbeat", "parent heartbeat succeeded", heartbeatErr)
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(recorder.result)
	}
	if recorder.firstErr != nil {
		return recorder.firstErr
	}
	return writeNativeOutput(command.OutOrStdout(), "OK: service, config permissions, local health, and parent heartbeat\n")
}

func recordNativePermissionChecks(recorder *nativeDoctorRecorder, paths nativeInstallPaths) {
	for _, file := range []struct{ name, path string }{{"config", paths.config}, {"credentials", paths.credentials}} {
		info, err := os.Stat(file.path)
		if err != nil {
			err = fmt.Errorf("%s: %w", file.name, err)
		} else if info.Mode().Perm()&0o037 != 0 {
			err = fmt.Errorf("%s permissions are too open: %o", file.name, info.Mode().Perm())
		}
		recorder.record(file.name, file.name+" permissions are secure", err)
	}
}

func nativeSandboxCheck(cfg nativeDaemonConfig) (string, error) {
	if !cfg.FilesystemSandbox.Enabled {
		return "filesystem sandbox is disabled", nil
	}
	if runtime.GOOS != "darwin" {
		return "filesystem sandbox is available", errors.New("filesystem sandbox is enabled but this host is not macOS")
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return "filesystem sandbox is available", fmt.Errorf("filesystem sandbox executable: %w", err)
	}
	return "filesystem sandbox is available", nil
}

func runNativeRestart(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	return restartNativeService(instance)
}

func runNativeUpdate(_ *cobra.Command, _ []string) error {
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
	active, err := filepath.Glob(filepath.Join(cfg.StateDir, "sessions", "*"))
	if err != nil {
		return fmt.Errorf("find active native sessions: %w", err)
	}
	if len(active) > 0 {
		return fmt.Errorf("refusing to update daemon with %d active session(s); drain them first", len(active))
	}
	if err := copyExecutable(paths.binary); err != nil {
		return fmt.Errorf("replace managed daemon binary: %w", err)
	}
	cfg.Version = nativeBuildVersion()
	cfg.ConnectionToken = ""
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode updated native daemon config: %w", err)
	}
	if err := atomicWriteFile(paths.config, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write updated native daemon config: %w", err)
	}
	if err := secureNativeConfig(paths.config); err != nil {
		return fmt.Errorf("secure updated native daemon config: %w", err)
	}
	if err := restartNativeService(instance); err != nil {
		return fmt.Errorf("restart native daemon: %w", err)
	}
	if err := waitNativeHealth(cfg.Listen, 30*time.Second); err != nil {
		return fmt.Errorf("updated daemon did not become healthy: %w", err)
	}
	return writeNativeOutput(os.Stdout, fmt.Sprintf("Native ESM instance %q updated and restarted\n", instance))
}

func runNativeRotateToken(_ *cobra.Command, _ []string) error {
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
	apiKey, err := readLifecycleAPIKey()
	if err != nil {
		return err
	}
	endpoint := nativeManagerEndpoint(cfg, "/registration-token")
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create token rotation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	var issued struct {
		RegistrationToken string `json:"registration_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		return err
	}
	registration, err := enrollNativeManager(cfg.UpstreamURL, map[string]interface{}{
		"registration_token": issued.RegistrationToken,
		"instance_id":        cfg.InstanceID,
		"name":               cfg.Name,
	})
	if err != nil {
		return err
	}
	if err := saveRotatedNativeCredentials(&cfg, registration.ConnectionToken, registration.Name); err != nil {
		return err
	}
	if err := restartNativeService(instance); err != nil {
		return err
	}
	return writeNativeOutput(os.Stdout, fmt.Sprintf("Connection token rotated and daemon %s restarted\n", nativeServiceName(instance)))
}

func saveRotatedNativeCredentials(cfg *nativeDaemonConfig, connectionToken, registeredName string) error {
	cfg.ConnectionToken = connectionToken
	if registeredName != "" {
		cfg.Name = registeredName
	}
	credentials, err := json.MarshalIndent(map[string]string{"connection_token": connectionToken}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode native credentials: %w", err)
	}
	if err := atomicWriteFile(cfg.CredentialsPath, append(credentials, '\n'), 0o600); err != nil {
		return err
	}
	return secureNativeConfig(cfg.CredentialsPath)
}

func runNativeUninstall(_ *cobra.Command, _ []string) error {
	instance, err := resolveNativeInstance()
	if err != nil {
		return err
	}
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	active, err := waitForNativeSessions(cfg.StateDir)
	if err != nil {
		return err
	}
	if len(active) > 0 && !nativeManageOpts.force {
		return fmt.Errorf("refusing to uninstall: %d active session(s); use --drain or --force", len(active))
	}
	if len(active) > 0 && nativeManageOpts.force {
		if err := terminateNativeSessions(active); err != nil {
			return err
		}
	}
	cleanupErr := removeNativeService(paths)
	if !nativeManageOpts.keepRegistration && cfg.ManagerID != "" {
		cleanupErr = errors.Join(cleanupErr, deleteNativeRegistration(cfg))
	}
	if !nativeManageOpts.keepData {
		cleanupErr = errors.Join(cleanupErr, removeNativeData(instance, paths, cfg.StateDir))
	}
	message := fmt.Sprintf("Native ESM instance %q uninstalled\n", instance)
	return errors.Join(cleanupErr, writeNativeOutput(os.Stdout, message))
}

func waitForNativeSessions(stateDir string) ([]string, error) {
	active, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*"))
	if err != nil || len(active) == 0 || !nativeManageOpts.drain {
		return active, err
	}
	timeout, err := time.ParseDuration(nativeManageOpts.drainTimeout)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for len(active) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		active, err = filepath.Glob(filepath.Join(stateDir, "sessions", "*"))
		if err != nil {
			return nil, fmt.Errorf("refresh active native sessions: %w", err)
		}
	}
	return active, nil
}

func nativeManagerEndpoint(cfg nativeDaemonConfig, suffix string) string {
	query := url.Values{}
	if cfg.Scope != "" {
		query.Set("scope", cfg.Scope)
	}
	if cfg.TeamID != "" {
		query.Set("team_id", cfg.TeamID)
	}
	endpoint := strings.TrimRight(cfg.UpstreamURL, "/") + "/session-managers/" + url.PathEscape(cfg.ManagerID) + suffix
	if encoded := query.Encode(); encoded != "" {
		return endpoint + "?" + encoded
	}
	return endpoint
}

func deleteNativeRegistration(cfg nativeDaemonConfig) error {
	apiKey, err := readLifecycleAPIKey()
	if err != nil {
		return fmt.Errorf("service removed; registration not removed: %w", err)
	}
	request, err := http.NewRequest(http.MethodDelete, nativeManagerEndpoint(cfg, ""), nil)
	if err != nil {
		return fmt.Errorf("create registration delete request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	closeutils.SafeCloseResponseBody(response.Body)
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("registration delete returned HTTP %d", response.StatusCode)
	}
	return nil
}

func removeNativeData(instance string, paths nativeInstallPaths, stateDir string) error {
	var cleanupErr error
	// The managed binary can be shared by other instances.
	if !nativeBinaryUsedByOtherInstances(instance, paths.binary) {
		cleanupErr = removeNativePath(paths.binary)
	}
	cleanupErr = errors.Join(cleanupErr, removeNativePath(paths.config), removeNativePath(paths.credentials))
	if safeNativeStateDir(stateDir) {
		cleanupErr = errors.Join(cleanupErr, os.RemoveAll(stateDir))
	}
	return cleanupErr
}

func writeNativeOutput(writer interface{ Write([]byte) (int, error) }, message string) error {
	written, err := writer.Write([]byte(message))
	if err != nil {
		return err
	}
	if written != len(message) {
		return fmt.Errorf("short native command output write: wrote %d of %d bytes", written, len(message))
	}
	return nil
}

func removeNativeService(paths nativeInstallPaths) error {
	if runtime.GOOS == "linux" {
		return errors.Join(
			runCommand("systemctl", "disable", "--now", nativeServiceUnitName(paths.service)),
			removeNativePath(paths.service),
			runCommand("systemctl", "daemon-reload"),
		)
	}
	return errors.Join(
		runCommand("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+nativeLaunchLabel(paths.service)),
		removeNativePath(paths.service),
	)
}

func removeNativePath(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
