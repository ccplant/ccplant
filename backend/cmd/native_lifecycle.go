package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func terminateNativeSessions(sessionDirs []string) error {
	var terminateErr error
	for _, dir := range sessionDirs {
		data, err := os.ReadFile(filepath.Join(dir, "runtime", "state.json"))
		if err != nil {
			continue
		}
		var state struct {
			PID int `json:"pid"`
		}
		if json.Unmarshal(data, &state) == nil && state.PID > 1 {
			if err := syscall.Kill(-state.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				terminateErr = errors.Join(terminateErr, fmt.Errorf("terminate native session %s: %w", dir, err))
			}
		}
	}
	time.Sleep(time.Second)
	return terminateErr
}

func safeNativeStateDir(path string) bool {
	clean := filepath.Clean(path)
	return clean != "/" && clean != "." && len(strings.Split(clean, string(filepath.Separator))) >= 3
}

// nativeBinaryUsedByOtherInstances reports whether any discovered instance
// other than the one being uninstalled references the same managed binary.
func nativeBinaryUsedByOtherInstances(currentInstance, binaryPath string) bool {
	return binarySharedWith(nativeDiscoverInstances(), currentInstance, binaryPath)
}

func binarySharedWith(entries []nativeInstanceListEntry, currentInstance, binaryPath string) bool {
	if binaryPath == "" {
		return false
	}
	for _, entry := range entries {
		if entry.Instance == currentInstance {
			continue
		}
		if entry.BinaryPath == binaryPath {
			return true
		}
	}
	return false
}

func enrollNativeManager(upstream string, payload interface{}) (*nativeRegistrationResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode native enrollment: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(upstream, "/")+"/session-managers/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var result nativeRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func sendNativeHeartbeat(cfg nativeDaemonConfig) error {
	body, err := json.Marshal(map[string]interface{}{"version": nativeBuildVersion()})
	if err != nil {
		return fmt.Errorf("encode native heartbeat: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.UpstreamURL, "/")+"/internal/session-managers/"+cfg.ManagerID+"/heartbeat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create native heartbeat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.ConnectionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
}

func readLifecycleAPIKey() (string, error) {
	var value string
	if nativeManageOpts.apiKeyStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 64*1024))
		if err != nil {
			return "", err
		}
		value = string(data)
	} else if nativeManageOpts.apiKeyFile != "" {
		data, err := os.ReadFile(nativeManageOpts.apiKeyFile)
		if err != nil {
			return "", err
		}
		value = string(data)
	} else {
		value = os.Getenv(nativeManageOpts.apiKeyEnv)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("API key is required for this lifecycle operation; use --api-key-stdin, --api-key-file, or --api-key-env")
	}
	return value, nil
}

func readNativeConfig(path string) (nativeDaemonConfig, error) {
	var cfg nativeDaemonConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ConnectionToken == "" && cfg.CredentialsPath != "" {
		credentialsData, readErr := os.ReadFile(cfg.CredentialsPath)
		if readErr != nil {
			return cfg, readErr
		}
		var credentials struct {
			ConnectionToken string `json:"connection_token"`
		}
		if err = json.Unmarshal(credentialsData, &credentials); err != nil {
			return cfg, err
		}
		cfg.ConnectionToken = credentials.ConnectionToken
	}
	return cfg, nil
}
func atomicWriteFile(path string, data []byte, mode os.FileMode) (returnErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".native-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove temporary native file: %w", err))
		}
	}()
	if err = tmp.Chmod(mode); err == nil {
		var written int
		written, err = tmp.Write(data)
		if err == nil && written != len(data) {
			err = io.ErrShortWrite
		}
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func copyExecutable(destination string) (returnErr error) {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve executable symlinks: %w", err)
	}
	if source == destination {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, in.Close()) }()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".agentapi-proxy-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove temporary executable: %w", err))
		}
	}()
	if err = tmp.Chmod(0o755); err == nil {
		var copied int64
		copied, err = io.Copy(tmp, in)
		if err == nil && copied == 0 {
			err = errors.New("managed daemon executable is empty")
		}
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, destination)
}
func stableNativeInstanceID(hostname, instance string) string {
	source := hostname
	if instance != "" && instance != nativeDefaultInstance {
		// Keep non-default instance IDs distinct from the default instance while
		// remaining stable across reinstalls of the same named instance.
		source += "\x00" + instance
	}
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		source += string(data)
	}
	sum := sha256.Sum256([]byte(source))
	return "native-" + sanitizeID(hostname) + "-" + hex.EncodeToString(sum[:4])
}
func sanitizeID(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
func formatLabels(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}
func responseError(resp *http.Response) error {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("HTTP %d (read response: %w)", resp.StatusCode, err)
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
}
func nativeHealth(listen string) error {
	address := listen
	if strings.HasPrefix(address, ":") {
		address = "127.0.0.1" + address
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + address + "/healthz")
	if err != nil {
		return err
	}
	defer closeutils.SafeCloseResponseBody(resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func waitNativeHealth(listen string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if nativeHealth(listen) == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("native daemon did not become healthy")
}
func runCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// nativeServiceName returns the OS-native service identifier (systemd unit or
// launchd label) for the given instance. The default instance preserves the
// historical identifiers so existing installations keep working unchanged.
func nativeServiceName(instance string) string {
	return nativeServiceNameFor(runtime.GOOS, instance)
}

func nativeServiceNameFor(goos, instance string) string {
	if instance == "" {
		instance = nativeDefaultInstance
	}
	if goos == "linux" {
		if instance == nativeDefaultInstance {
			return "agentapi-native.service"
		}
		return "agentapi-native-" + instance + ".service"
	}
	if instance == nativeDefaultInstance {
		return "com.agentapi.native"
	}
	return "com.agentapi.native." + instance
}

// nativeServiceUnitName derives the systemd unit name from a service file path.
func nativeServiceUnitName(servicePath string) string { return filepath.Base(servicePath) }

// nativeLaunchLabel derives the launchd label from a plist service file path.
func nativeLaunchLabel(servicePath string) string {
	return strings.TrimSuffix(filepath.Base(servicePath), ".plist")
}

func nativeServiceRunning(instance string) bool {
	var command *exec.Cmd
	if runtime.GOOS == "linux" {
		command = exec.Command("systemctl", "is-active", "--quiet", nativeServiceName(instance))
	} else {
		command = exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+nativeServiceName(instance))
	}
	return command.Run() == nil
}
func restartNativeService(instance string) error {
	paths, err := nativePaths(nativeManageOpts.configPath, instance)
	if err != nil {
		return err
	}
	cfg, err := readNativeConfig(paths.config)
	if err != nil {
		return fmt.Errorf("read native daemon config: %w", err)
	}
	if runtime.GOOS == "linux" {
		unit := renderNativeSystemdUnit(paths, cfg.ManagerEnvironment)
		if err := atomicWriteFile(paths.service, []byte(unit), 0o644); err != nil {
			return fmt.Errorf("refresh native service environment: %w", err)
		}
		if err := runCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return runCommand("systemctl", "restart", nativeServiceName(instance))
	}
	plist := renderNativeLaunchAgent(paths, cfg.ManagerEnvironment)
	if err := atomicWriteFile(paths.service, []byte(plist), 0o600); err != nil {
		return fmt.Errorf("refresh native service environment: %w", err)
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if err := runCommand("launchctl", "bootout", domain+"/"+nativeServiceName(instance)); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return fmt.Errorf("unload native launch service: %w", err)
		}
	}
	return runCommand("launchctl", "bootstrap", domain, paths.service)
}
func ensureLinuxServiceUser() error {
	if exec.Command("id", "-u", "agentapi").Run() == nil {
		return nil
	}
	return runCommand("useradd", "--system", "--home-dir", "/var/lib/agentapi-native", "--shell", "/usr/sbin/nologin", "agentapi")
}
func lookupUID(name string) int {
	out, err := exec.Command("id", "-u", name).Output()
	if err != nil {
		return -1
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return value
}
func lookupGID(name string) int {
	out, err := exec.Command("id", "-g", name).Output()
	if err != nil {
		return -1
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return -1
	}
	return value
}
func secureNativeConfig(path string) error {
	if runtime.GOOS != "linux" {
		return os.Chmod(path, 0o600)
	}
	if err := os.Chown(path, 0, lookupGID("agentapi")); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}
func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

// nativeInstanceListEntry describes one discovered native ESM instance for
// `native list`.
