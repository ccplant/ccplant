package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type nativeInstallPaths struct{ config, credentials, state, binary, service, logDir string }

func nativePaths(configOverride, instance string) (nativeInstallPaths, error) {
	if runtime.GOOS == "linux" && os.Geteuid() != 0 && configOverride == "" {
		return nativeInstallPaths{}, errors.New("native install on Linux must run as root (use sudo)")
	}
	paths, err := nativeInstallPathsFor(runtime.GOOS, instance, configOverride)
	if err != nil {
		return nativeInstallPaths{}, err
	}
	return paths, nil
}

// nativeInstallPathsFor computes the install paths for an instance on the given
// OS without touching the filesystem or checking privileges, which makes it
// testable from non-root test processes. The default instance preserves the
// historical paths and service names exactly.
func nativeInstallPathsFor(goos, instance, configOverride string) (nativeInstallPaths, error) {
	if instance == "" {
		instance = nativeDefaultInstance
	}
	isDefault := instance == nativeDefaultInstance
	if goos == "linux" {
		var configDir, credentials, state, logDir string
		binary := "/usr/local/libexec/agentapi-proxy/ccplant"
		if isDefault {
			configDir = "/etc/agentapi-native"
			credentials = "/etc/agentapi-native/credentials.json"
			state = "/var/lib/agentapi-native"
			logDir = "/var/log/agentapi-native"
		} else {
			configDir = "/etc/agentapi-native-" + instance
			credentials = filepath.Join(configDir, "credentials.json")
			state = "/var/lib/agentapi-native-" + instance
			logDir = "/var/log/agentapi-native-" + instance
		}
		config := filepath.Join(configDir, "config.json")
		if configOverride != "" {
			config = configOverride
		}
		service := filepath.Join("/etc/systemd/system", nativeServiceNameFor(goos, instance))
		return nativeInstallPaths{config: config, credentials: credentials, state: state, binary: binary, service: service, logDir: logDir}, nil
	}
	if goos == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nativeInstallPaths{}, err
		}
		var base, logDir string
		if isDefault {
			base = filepath.Join(home, "Library", "Application Support", "agentapi-native")
			logDir = filepath.Join(home, "Library", "Logs", "agentapi-native")
		} else {
			base = filepath.Join(home, "Library", "Application Support", "agentapi-native-"+instance)
			logDir = filepath.Join(home, "Library", "Logs", "agentapi-native-"+instance)
		}
		config := filepath.Join(base, "config.json")
		if configOverride != "" {
			config = configOverride
		}
		return nativeInstallPaths{config: config, credentials: filepath.Join(base, "credentials.json"), state: filepath.Join(base, "state"), binary: filepath.Join(base, "bin", "ccplant"), service: filepath.Join(home, "Library", "LaunchAgents", nativeServiceNameFor(goos, instance)+".plist"), logDir: logDir}, nil
	}
	return nativeInstallPaths{}, fmt.Errorf("unsupported OS: %s", goos)
}

func installNativeService(paths nativeInstallPaths, cfg nativeDaemonConfig) error {
	for _, dir := range []string{filepath.Dir(paths.config), paths.state, filepath.Dir(paths.binary), paths.logDir, filepath.Dir(paths.service)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if runtime.GOOS == "linux" {
		if err := ensureLinuxServiceUser(); err != nil {
			return err
		}
		uid, gid := lookupUID("agentapi"), lookupGID("agentapi")
		if err := filepath.Walk(paths.state, func(path string, _ os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			return os.Chown(path, uid, gid)
		}); err != nil {
			return err
		}
	}
	if err := copyExecutable(paths.binary); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		uid, gid := lookupUID("agentapi"), lookupGID("agentapi")
		// The daemon replaces this checksummed binary during dashboard-initiated
		// upgrades. The directory contains only the managed native ESM executable.
		if err := os.Chown(filepath.Dir(paths.binary), uid, gid); err != nil {
			return err
		}
		if err := os.Chown(paths.binary, uid, gid); err != nil {
			return err
		}
	}
	cfg.CredentialsPath = paths.credentials
	credentials, _ := json.MarshalIndent(map[string]string{"connection_token": cfg.ConnectionToken}, "", "  ")
	if err := atomicWriteFile(paths.credentials, append(credentials, '\n'), 0o600); err != nil {
		return err
	}
	cfg.ConnectionToken = ""
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := atomicWriteFile(paths.config, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if err := secureNativeConfig(paths.credentials); err != nil {
			return err
		}
		if err := secureNativeConfig(paths.config); err != nil {
			return err
		}
		unit := renderNativeSystemdUnit(paths, cfg.ManagerEnvironment)
		if err := atomicWriteFile(paths.service, []byte(unit), 0o644); err != nil {
			return err
		}
		if err := runCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := runCommand("systemctl", "enable", nativeServiceUnitName(paths.service)); err != nil {
			return err
		}
		return runCommand("systemctl", "restart", nativeServiceUnitName(paths.service))
	}
	plist := renderNativeLaunchAgent(paths, cfg.ManagerEnvironment)
	if err := atomicWriteFile(paths.service, []byte(plist), 0o600); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = runCommand("launchctl", "bootout", domain+"/"+nativeLaunchLabel(paths.service))
	return runCommand("launchctl", "bootstrap", domain, paths.service)
}

func parseNativeManagerEnvironment(raw string) (string, string, error) {
	key, value, ok := strings.Cut(raw, "=")
	if !ok || !validEnvironmentKey(key) {
		return "", "", fmt.Errorf("invalid --manager-env %q; expected KEY=VALUE", raw)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", "", fmt.Errorf("invalid --manager-env %q; value must not contain NUL or newlines", raw)
	}
	return key, value, nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || (!asciiLetter(key[0]) && key[0] != '_') {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !asciiLetter(key[i]) && (key[i] < '0' || key[i] > '9') && key[i] != '_' {
			return false
		}
	}
	return true
}

func asciiLetter(value byte) bool {
	return (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}

func sortedEnvironmentKeys(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func renderNativeSystemdUnit(paths nativeInstallPaths, environment map[string]string) string {
	var env strings.Builder
	for _, key := range sortedEnvironmentKeys(environment) {
		value := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(environment[key])
		fmt.Fprintf(&env, "Environment=\"%s=%s\"\n", key, value)
	}
	return fmt.Sprintf("[Unit]\nDescription=agentapi-proxy native external session manager\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=agentapi\nGroup=agentapi\n%sExecStart=%s native-session-manager --config %s\nRestart=always\nRestartSec=3\nKillMode=process\nTimeoutStopSec=30\nLimitNOFILE=65536\nStandardOutput=append:%s/native.log\nStandardError=append:%s/native.log\n\n[Install]\nWantedBy=multi-user.target\n", env.String(), paths.binary, paths.config, paths.logDir, paths.logDir)
}

func renderNativeLaunchAgent(paths nativeInstallPaths, environment map[string]string) string {
	label := nativeLaunchLabel(paths.service)
	var env strings.Builder
	if len(environment) > 0 {
		env.WriteString("<key>EnvironmentVariables</key><dict>")
		for _, key := range sortedEnvironmentKeys(environment) {
			fmt.Fprintf(&env, "<key>%s</key><string>%s</string>", xmlEscape(key), xmlEscape(environment[key]))
		}
		env.WriteString("</dict>")
	}
	return fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>native-session-manager</string><string>--config</string><string>%s</string></array>%s<key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>%s/native.log</string><key>StandardErrorPath</key><string>%s/native.log</string></dict></plist>\n", xmlEscape(label), xmlEscape(paths.binary), xmlEscape(paths.config), env.String(), xmlEscape(paths.logDir), xmlEscape(paths.logDir))
}
