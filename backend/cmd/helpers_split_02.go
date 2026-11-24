package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/mcp"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"github.com/takutakahashi/agentapi-proxy/pkg/settings"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

var (
	mcpInputDirs    string
	mcpOutputPath   string
	mcpExpandEnv    bool
	mcpMergeVerbose bool
)

var mergeMCPConfigCmd = &cobra.Command{
	Use:   "merge-mcp-config",
	Short: "Merge multiple MCP server configuration directories",
	Long: `Merge multiple MCP server configuration directories into a single JSON file.

This command reads MCP server configurations from multiple directories and merges
them into a single configuration file. Later directories take precedence over
earlier ones, allowing for base/team/user layered configuration.

Each directory should contain one or more JSON files with the mcpServers structure:
{
  "mcpServers": {
    "server-name": {
      "type": "http",
      "url": "https://example.com/mcp"
    }
  }
}

Environment variables can be expanded in the configuration using ${VAR} or
${VAR:-default} syntax when --expand-env is specified.

Examples:
  # Merge base, team, and user configs
  agentapi-proxy helpers merge-mcp-config \
    --input-dirs /mcp-config/base,/mcp-config/team,/mcp-config/user \
    --output /mcp-config/merged.json

  # With environment variable expansion
  agentapi-proxy helpers merge-mcp-config \
    --input-dirs /mcp-config/base,/mcp-config/user \
    --output /mcp-config/merged.json \
    --expand-env`,
	RunE: runMergeMCPConfig,
}

func init() {
	mergeMCPConfigCmd.Flags().StringVar(&mcpInputDirs, "input-dirs", "",
		"Comma-separated list of input directories (in priority order, later takes precedence)")
	mergeMCPConfigCmd.Flags().StringVarP(&mcpOutputPath, "output", "o", "",
		"Output file path for merged configuration")
	mergeMCPConfigCmd.Flags().BoolVar(&mcpExpandEnv, "expand-env", false,
		"Expand ${VAR} and ${VAR:-default} environment variable patterns")
	mergeMCPConfigCmd.Flags().BoolVarP(&mcpMergeVerbose, "verbose", "v", false,
		"Verbose output")

	if err := mergeMCPConfigCmd.MarkFlagRequired("input-dirs"); err != nil {
		panic(err)
	}
	if err := mergeMCPConfigCmd.MarkFlagRequired("output"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(mergeMCPConfigCmd)
}

func runMergeMCPConfig(cmd *cobra.Command, args []string) error {
	dirs := strings.Split(mcpInputDirs, ",")

	// Trim whitespace
	for i := range dirs {
		dirs[i] = strings.TrimSpace(dirs[i])
	}

	opts := mcp.MergeOptions{
		ExpandEnv: mcpExpandEnv,
		Verbose:   mcpMergeVerbose,
	}

	if mcpMergeVerbose {
		opts.Logger = func(format string, args ...interface{}) {
			fmt.Printf(format+"\n", args...)
		}
	}

	config, err := mcp.MergeConfigs(dirs, opts)
	if err != nil {
		return fmt.Errorf("failed to merge configs: %w", err)
	}

	if err := mcp.WriteConfig(config, mcpOutputPath); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	serverCount := len(config.MCPServers)
	if mcpMergeVerbose {
		fmt.Printf("[MCP] Merged %d server(s) to %s\n", serverCount, mcpOutputPath)
	} else {
		fmt.Printf("Successfully merged %d MCP server(s) to %s\n", serverCount, mcpOutputPath)
	}

	return nil
}

// sync command flags
var (
	syncSettingsFile              string
	syncOutputDir                 string
	syncCredentialsFile           string
	syncNotificationSubscriptions string
	syncNotificationsDir          string
	syncRegisterMarketplaces      bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Claude configuration from Settings Secret",
	Long: `Sync Claude configuration from a mounted Settings Secret.

This command reads settings from a mounted Settings Secret and generates:
- ~/.claude.json with onboarding settings
- ~/.claude/settings.json with marketplace configuration
- ~/.claude/plugins/marketplaces/ (cloned marketplace repositories)
- ~/.claude/.credentials.json (if credentials file is provided)
- ~/.claude/CLAUDE.md (copied from Docker image)
- Notification subscriptions (if provided)

It also clones any configured marketplace repositories to ~/.claude/plugins/marketplaces/
and optionally registers them using the claude CLI.

The settings file should be the mounted settings.json from the agentapi-settings-{user} Secret.
The credentials file should be the mounted credentials.json from the agentapi-agent-credentials-{user} Secret.

Examples:
  # Basic usage with defaults
  agentapi-proxy helpers sync

  # Specify all paths and register marketplaces
  agentapi-proxy helpers sync \
    --settings-file /settings-config/settings.json \
    --output-dir /home/agentapi \
    --credentials-file /credentials-config/credentials.json \
    --notification-subscriptions /notification-subscriptions-source \
    --notifications-dir /notifications \
    --register-marketplaces`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().StringVar(&syncSettingsFile, "settings-file", "/settings-config/settings.json",
		"Path to the mounted settings.json from Settings Secret")
	syncCmd.Flags().StringVar(&syncOutputDir, "output-dir", "",
		"Output directory (home directory, defaults to $HOME)")
	syncCmd.Flags().StringVar(&syncCredentialsFile, "credentials-file", "",
		"Path to the mounted credentials.json from Credentials Secret (optional)")
	syncCmd.Flags().StringVar(&syncNotificationSubscriptions, "notification-subscriptions", "",
		"Path to notification subscriptions directory (optional)")
	syncCmd.Flags().StringVar(&syncNotificationsDir, "notifications-dir", "",
		"Path to notifications output directory (optional)")
	syncCmd.Flags().BoolVar(&syncRegisterMarketplaces, "register-marketplaces", false,
		"Register cloned marketplaces using claude CLI")

	HelpersCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	outputDir := syncOutputDir
	if outputDir == "" {
		var err error
		outputDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	opts := startup.SyncOptions{
		SettingsFile:              syncSettingsFile,
		OutputDir:                 outputDir,
		CredentialsFile:           syncCredentialsFile,
		NotificationSubscriptions: syncNotificationSubscriptions,
		NotificationsDir:          syncNotificationsDir,
		RegisterMarketplaces:      syncRegisterMarketplaces,
	}

	if err := startup.Sync(opts); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Println("Claude configuration sync completed successfully!")
	return nil
}

// merge-settings-config command flags
var (
	settingsInputDirs    string
	settingsOutputPath   string
	settingsMergeVerbose bool
)

var mergeSettingsConfigCmd = &cobra.Command{
	Use:   "merge-settings-config",
	Short: "Merge multiple settings configuration directories",
	Long: `Merge multiple settings configuration directories into a single JSON file.

This command reads settings configurations (marketplaces and enabled plugins) from
multiple directories and merges them into a single configuration file.

For marketplaces, later directories take precedence over earlier ones (last wins).
For enabled plugins, all unique plugins from all directories are merged (union).

This allows for base/team/user layered configuration where:
- Base settings provide organization-wide defaults
- Team settings can override or extend base settings
- User settings have the highest priority

Each directory should contain one or more JSON files with the settings structure:
{
  "name": "settings-name",
  "marketplaces": {
    "marketplace-name": {
      "url": "https://github.com/org/marketplace"
    }
  },
  "enabled_plugins": [
    "plugin-name@marketplace-name"
  ]
}

Examples:
  # Merge base, team, and user configs
  agentapi-proxy helpers merge-settings-config \
    --input-dirs /settings-config/base,/settings-config/team/0,/settings-config/user \
    --output /settings-config/settings.json

  # With verbose output
  agentapi-proxy helpers merge-settings-config \
    --input-dirs /settings-config/base,/settings-config/user \
    --output /settings-config/settings.json \
    --verbose`,
	RunE: runMergeSettingsConfig,
}

func init() {
	mergeSettingsConfigCmd.Flags().StringVar(&settingsInputDirs, "input-dirs", "",
		"Comma-separated list of input directories (in priority order, later takes precedence)")
	mergeSettingsConfigCmd.Flags().StringVarP(&settingsOutputPath, "output", "o", "",
		"Output file path for merged configuration")
	mergeSettingsConfigCmd.Flags().BoolVarP(&settingsMergeVerbose, "verbose", "v", false,
		"Verbose output")

	if err := mergeSettingsConfigCmd.MarkFlagRequired("input-dirs"); err != nil {
		panic(err)
	}
	if err := mergeSettingsConfigCmd.MarkFlagRequired("output"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(mergeSettingsConfigCmd)
}

func runMergeSettingsConfig(cmd *cobra.Command, args []string) error {
	dirs := strings.Split(settingsInputDirs, ",")

	// Trim whitespace
	for i := range dirs {
		dirs[i] = strings.TrimSpace(dirs[i])
	}

	opts := settings.MergeOptions{
		Verbose: settingsMergeVerbose,
	}

	if settingsMergeVerbose {
		opts.Logger = func(format string, args ...interface{}) {
			fmt.Printf(format+"\n", args...)
		}
	}

	config, err := settings.MergeConfigs(dirs, opts)
	if err != nil {
		return fmt.Errorf("failed to merge settings configs: %w", err)
	}

	if err := settings.WriteConfig(config, settingsOutputPath); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	marketplaceCount := len(config.Marketplaces)
	pluginCount := len(config.EnabledPlugins)
	if settingsMergeVerbose {
		fmt.Printf("[SETTINGS] Merged %d marketplace(s) and %d plugin(s) to %s\n", marketplaceCount, pluginCount, settingsOutputPath)
	} else {
		fmt.Printf("Successfully merged %d marketplace(s) and %d plugin(s) to %s\n", marketplaceCount, pluginCount, settingsOutputPath)
	}

	return nil
}

// setup command flags
var (
	setupInputPath                 string
	setupSettingsFile              string
	setupCredentialsFile           string
	setupNotificationSubscriptions string
	setupNotificationsDir          string
	setupRegisterMarketplaces      bool
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run unified session setup (write-pem, clone-repo, compile-settings, sync-extra)",
	Long: `Run the full init-container setup sequence for a session Pod.

Reads /session-settings/settings.yaml and performs:
  1. write-pem  - writes GITHUB_APP_PEM to /github-app/app.pem
  2. clone-repo - clones the repository (if session.repository is set)
  3. compile    - generates .claude.json, settings.json, mcp config, env file, startup script
  4. sync-extra - copies credentials, notification subscriptions

This replaces the old write-pem, clone-repo, merge-settings, sync-config, and setup-mcp
init containers with a single unified step.

Examples:
  # Use defaults (reads /session-settings/settings.yaml)
  agentapi-proxy helpers setup

  # Custom paths
  agentapi-proxy helpers setup \
    --input /session-settings/settings.yaml \
    --credentials-file /credentials-config/credentials.json \
    --notification-subscriptions /notification-subscriptions-source \
    --notifications-dir /notifications \
    --register-marketplaces`,
	RunE: runSetup,
}

func init() {
	defaults := sessionsettings.DefaultSetupOptions()
	compileDefaults := sessionsettings.DefaultCompileOptions()

	setupCmd.Flags().StringVar(&setupInputPath, "input", defaults.InputPath,
		"Path to the session settings YAML file")
	setupCmd.Flags().StringVar(&setupSettingsFile, "settings-file", "",
		"Path to user settings.json with marketplace/plugin config (optional)")
	setupCmd.Flags().StringVar(&setupCredentialsFile, "credentials-file", "",
		"Path to credentials.json from Credentials Secret (optional)")
	setupCmd.Flags().StringVar(&setupNotificationSubscriptions, "notification-subscriptions", "",
		"Source directory for notification subscription files (optional)")
	setupCmd.Flags().StringVar(&setupNotificationsDir, "notifications-dir", "",
		"Destination directory for notification files (optional)")
	setupCmd.Flags().BoolVar(&setupRegisterMarketplaces, "register-marketplaces", false,
		"Register cloned marketplace repos via claude CLI")

	_ = compileDefaults // referenced via DefaultCompileOptions inside Setup()
	HelpersCmd.AddCommand(setupCmd)
}
