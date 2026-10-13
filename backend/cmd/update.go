package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	"github.com/takutakahashi/agentapi-proxy/internal/updater"
)

var updateJSON bool

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print ccplant build information",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, _ []string) error {
		information := struct {
			Version      string `json:"version"`
			Distribution string `json:"distribution"`
			Platform     string `json:"platform"`
		}{Version: buildinfo.Version, Distribution: buildinfo.Distribution, Platform: runtime.GOOS + "/" + runtime.GOARCH}
		if updateJSON {
			return json.NewEncoder(command.OutOrStdout()).Encode(information)
		}
		_, err := fmt.Fprintf(command.OutOrStdout(), "ccplant %s (%s; %s)\n", information.Version, information.Distribution, information.Platform)
		return err
	},
}

var UpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Manage standalone ccplant CLI updates",
	Args:  cobra.NoArgs,
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for a newer stable ccplant release",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, _ []string) error {
		service := newCLIUpdater()
		if !service.Eligible() {
			return errors.New("self-update is available only for supported GitHub Release binaries")
		}
		ctx, cancel := context.WithTimeout(command.Context(), 5*time.Second)
		defer cancel()
		result, err := service.Check(ctx)
		if err != nil {
			return err
		}
		return writeUpdateResult(command, result)
	},
}

var updateApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Download, verify, and install the latest stable release",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, _ []string) error {
		service := newCLIUpdater()
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		result, err := service.Apply(ctx)
		if err != nil {
			return err
		}
		state, stateErr := service.LoadState()
		if stateErr == nil {
			now := time.Now().UTC()
			state.LastCheckedAt = &now
			state.LatestVersion = result.LatestVersion
			state.LastErrorAt = nil
			state.LastErrorCode = ""
			if result.Updated {
				state.LastInstalledVersion = result.LatestVersion
			}
			stateErr = service.SaveState(state)
		}
		if stateErr != nil {
			return fmt.Errorf("save update state: %w", stateErr)
		}
		return writeUpdateResult(command, result)
	},
}

func newUpdateEnabledCommand(enabled bool) *cobra.Command {
	verb := "enable"
	description := "Enable automatic update checks"
	if !enabled {
		verb = "disable"
		description = "Disable automatic update checks"
	}
	return &cobra.Command{Use: verb, Short: description, Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		state, err := newCLIUpdater().SetEnabled(enabled)
		if err != nil {
			return err
		}
		if updateJSON {
			return json.NewEncoder(command.OutOrStdout()).Encode(state)
		}
		_, err = fmt.Fprintf(command.OutOrStdout(), "Automatic updates %s.\n", map[bool]string{true: "enabled", false: "disabled"}[enabled])
		return err
	}}
}

var updateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show automatic update configuration and state",
	Args:  cobra.NoArgs,
	RunE: func(command *cobra.Command, _ []string) error {
		service := newCLIUpdater()
		state, err := service.LoadState()
		if err != nil {
			return err
		}
		status := struct {
			Version      string        `json:"version"`
			Distribution string        `json:"distribution"`
			Platform     string        `json:"platform"`
			Eligible     bool          `json:"eligible"`
			State        updater.State `json:"state"`
		}{buildinfo.Version, buildinfo.Distribution, runtime.GOOS + "/" + runtime.GOARCH, service.Eligible(), state}
		if updateJSON {
			return json.NewEncoder(command.OutOrStdout()).Encode(status)
		}
		_, err = fmt.Fprintf(command.OutOrStdout(), "Version: %s\nDistribution: %s\nPlatform: %s\nEligible: %t\nEnabled: %t\nLatest: %s\nLast error: %s\n", status.Version, status.Distribution, status.Platform, status.Eligible, state.Enabled, state.LatestVersion, state.LastErrorCode)
		return err
	},
}

func init() {
	VersionCmd.Flags().BoolVar(&updateJSON, "json", false, "print JSON output")
	UpdateCmd.PersistentFlags().BoolVar(&updateJSON, "json", false, "print JSON output")
	UpdateCmd.AddCommand(updateCheckCmd, updateApplyCmd, newUpdateEnabledCommand(true), newUpdateEnabledCommand(false), updateStatusCmd)
}

func newCLIUpdater() *updater.Updater {
	manifestURL := ""
	if buildinfo.Distribution == "dev" {
		manifestURL = os.Getenv("CCPLANT_UPDATE_MANIFEST_URL")
	}
	return updater.New(updater.Options{CurrentVersion: buildinfo.Version, Distribution: buildinfo.Distribution, PublicKey: buildinfo.UpdatePublicKey, ManifestURL: manifestURL})
}

func writeUpdateResult(command *cobra.Command, result updater.Result) error {
	if updateJSON {
		return json.NewEncoder(command.OutOrStdout()).Encode(result)
	}
	if result.Updated {
		_, err := fmt.Fprintf(command.OutOrStdout(), "Updated ccplant from %s to %s. The new version will be used on the next invocation.\n", result.CurrentVersion, result.LatestVersion)
		return err
	}
	_, err := fmt.Fprintf(command.OutOrStdout(), "ccplant %s is current (latest: %s).\n", result.CurrentVersion, result.LatestVersion)
	return err
}

// RunAutomaticUpdate is intentionally fail-open. An update service outage must
// never change the requested command's output or exit status.
func RunAutomaticUpdate(command *cobra.Command, _ []string) error {
	if !automaticUpdateAllowed(command) {
		return nil
	}
	ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
	defer cancel()
	_, err := newCLIUpdater().AutoApply(ctx)
	if err != nil && terminalStderr() {
		_, _ = fmt.Fprintf(command.ErrOrStderr(), "ccplant: automatic update skipped: %s\n", err)
	}
	return nil
}

func automaticUpdateAllowed(command *cobra.Command) bool {
	parts := strings.Fields(command.CommandPath())
	if len(parts) < 2 {
		return false
	}
	switch parts[1] {
	case "doctor", "helm", "control-plane", "admin":
		return true
	case "client":
		if len(parts) < 3 {
			return false
		}
		switch parts[2] {
		case "cycle", "report-usage", "consume-secret", "send", "events", "backup-session-state":
			return false
		default:
			return true
		}
	default:
		return false
	}
}

func terminalStderr() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
