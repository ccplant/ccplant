package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type nativeInstanceListEntry struct {
	Instance   string `json:"instance"`
	Service    string `json:"service"`
	ManagerID  string `json:"manager_id"`
	Name       string `json:"name"`
	Upstream   string `json:"upstream"`
	State      string `json:"state"`
	BinaryPath string `json:"binary_path,omitempty"`
	Config     string `json:"config"`
	Running    bool   `json:"running"`
}

func runNativeList(command *cobra.Command, _ []string) error {
	entries := nativeDiscoverInstances()
	if nativeManageOpts.jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(entries)
	}
	w := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "INSTANCE\tNAME\tSERVICE\tMANAGER ID\tSTATE\tRUNNING")
	for _, entry := range entries {
		running := "no"
		if entry.Running {
			running = "yes"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", entry.Instance, firstNonEmpty(entry.Name, "-"), entry.Service, entry.ManagerID, entry.State, running)
	}
	return w.Flush()
}

// nativeInstanceBaseDirs returns the default instance config path and the glob
// pattern matching named-instance config paths for the current platform.
func nativeInstanceBaseDirs() (defaultConfig, pattern string) {
	return nativeInstanceBaseDirsFor(runtime.GOOS)
}

func nativeInstanceBaseDirsFor(goos string) (defaultConfig, pattern string) {
	if goos == "linux" {
		return "/etc/agentapi-native/config.json", "/etc/agentapi-native-*/config.json"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	base := filepath.Join(home, "Library", "Application Support")
	return filepath.Join(base, "agentapi-native", "config.json"), filepath.Join(base, "agentapi-native-*", "config.json")
}

// nativeDiscoverInstances enumerates installed native ESM instances on this
// host by scanning the well-known config directories. Missing or unreadable
// configs are skipped so a partially uninstalled host still lists the rest.
func nativeDiscoverInstances() []nativeInstanceListEntry {
	defaultConfig, pattern := nativeInstanceBaseDirs()
	return discoverNativeInstances(defaultConfig, pattern)
}

func discoverNativeInstances(defaultConfig, pattern string) []nativeInstanceListEntry {
	entries := make([]nativeInstanceListEntry, 0)
	seen := make(map[string]bool)
	add := func(name, configPath string) {
		if name == "" || seen[configPath] || configPath == "" {
			return
		}
		seen[configPath] = true
		cfg, err := readNativeConfig(configPath)
		if err != nil {
			return
		}
		entries = append(entries, nativeInstanceListEntry{
			Instance: name, Service: nativeServiceName(name), ManagerID: cfg.ManagerID, Name: cfg.Name,
			Upstream: cfg.UpstreamURL, State: cfg.StateDir,
			BinaryPath: cfg.BinaryPath, Config: configPath, Running: nativeServiceRunning(name),
		})
	}
	add(nativeDefaultInstance, defaultConfig)
	if pattern != "" {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			name := strings.TrimPrefix(filepath.Base(filepath.Dir(match)), "agentapi-native-")
			add(name, match)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Instance == nativeDefaultInstance && entries[j].Instance != nativeDefaultInstance {
			return true
		}
		if entries[j].Instance == nativeDefaultInstance {
			return false
		}
		return entries[i].Instance < entries[j].Instance
	})
	return entries
}
