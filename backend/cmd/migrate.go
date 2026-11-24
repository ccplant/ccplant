package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// migrateSettingsJSON is the JSON representation of settings stored in agentapi-settings-* Secrets.
// It mirrors the private settingsJSON in the repositories package.
type migrateSettingsJSON struct {
	Name                 string                             `json:"name"`
	Bedrock              *migrateBedrockJSON                `json:"bedrock,omitempty"`
	MCPServers           map[string]*migrateMCPServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces         map[string]*migrateMarketplaceJSON `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken string                             `json:"claude_code_oauth_token,omitempty"`
	AuthMode             string                             `json:"auth_mode,omitempty"`
	EnabledPlugins       []string                           `json:"enabled_plugins,omitempty"`
	EnvVars              map[string]string                  `json:"env_vars,omitempty"`
	CreatedAt            time.Time                          `json:"created_at"`
	UpdatedAt            time.Time                          `json:"updated_at"`
}

type migrateBedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

type migrateMCPServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type migrateMarketplaceJSON struct {
	URL string `json:"url"`
}

// migrate command flags
var (
	migrateNamespace string
	migrateCleanup   bool
	migrateDryRun    bool
	migrateVerbose   bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate derived Secrets into agentapi-settings-* (unified source)",
	Long: `Migrate and verify that derived Kubernetes Secrets are no longer needed.

This command verifies that agentapi-settings-* Secrets already contain all
configuration data (MCP servers, marketplaces, env vars, credentials) that
was previously split into separate derived Secrets:

  - mcp-servers-{name}    (labeled agentapi.proxy/mcp-servers=true)
  - marketplaces-{name}   (labeled agentapi.proxy/marketplaces=true)
  - agent-env-{name}      (labeled agentapi.proxy/env=true)

Phase 1 (default): Lists all agentapi-settings-* Secrets and reports their
contents so you can verify all data is present.

Phase 2 (--cleanup): Deletes derived Secrets that are labeled as managed by
agentapi-proxy settings. Use --dry-run to preview before deleting.

Examples:
  # Step 1: Verify settings data is complete (no deletions)
  agentapi-proxy helpers migrate --namespace agentapi-ui

  # Step 2: Preview what would be deleted
  agentapi-proxy helpers migrate --namespace agentapi-ui --cleanup --dry-run

  # Step 3: Delete derived Secrets after verification
  agentapi-proxy helpers migrate --namespace agentapi-ui --cleanup`,
	RunE: runMigrate,
}

func init() {
	migrateCmd.Flags().StringVar(&migrateNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	migrateCmd.Flags().BoolVar(&migrateCleanup, "cleanup", false,
		"Delete derived Secrets (mcp-servers-*, marketplaces-*, agent-env-*) after verification")
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false,
		"Show cleanup targets without deleting (use with --cleanup)")
	migrateCmd.Flags().BoolVarP(&migrateVerbose, "verbose", "v", false,
		"Verbose output")

	HelpersCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	ctx := context.Background()
	if err := verifySettingsSecrets(ctx, client); err != nil {
		return err
	}
	if !migrateCleanup {
		printCleanupSkipped()
		return nil
	}
	return cleanupDerivedSecrets(ctx, client)
}

func verifySettingsSecrets(ctx context.Context, client kubernetes.Interface) error {
	fmt.Printf("=== Phase 1: Verifying agentapi-settings-* Secrets (namespace: %s) ===\n", migrateNamespace)
	settingsList, err := client.CoreV1().Secrets(migrateNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/settings=true",
	})
	if err != nil {
		return fmt.Errorf("failed to list settings secrets: %w", err)
	}
	fmt.Printf("Found %d agentapi-settings-* secret(s)\n", len(settingsList.Items))
	for _, secret := range settingsList.Items {
		rawJSON, ok := secret.Data["settings.json"]
		if !ok {
			fmt.Printf("  [WARN] %s: missing settings.json key\n", secret.Name)
			continue
		}
		var sj migrateSettingsJSON
		if err := json.Unmarshal(rawJSON, &sj); err != nil {
			fmt.Printf("  [WARN] %s: failed to parse settings.json: %v\n", secret.Name, err)
			continue
		}
		printSettingsSecret(secret.Name, sj)
	}
	return nil
}

func printSettingsSecret(secretName string, settings migrateSettingsJSON) {
	parts := []string{namedCount("mcp_servers", mcpServerNames(settings.MCPServers)), namedCount("marketplaces", marketplaceNames(settings.Marketplaces))}
	parts = append(parts, fmt.Sprintf("env_vars=%d", len(settings.EnvVars)))
	if settings.AuthMode != "" {
		parts = append(parts, fmt.Sprintf("auth_mode=%s", settings.AuthMode))
	}
	if settings.ClaudeCodeOAuthToken != "" {
		parts = append(parts, "oauth_token=set")
	}
	if settings.Bedrock != nil && settings.Bedrock.Enabled {
		parts = append(parts, "bedrock=enabled")
	}
	if len(settings.EnabledPlugins) > 0 {
		parts = append(parts, fmt.Sprintf("plugins=%d", len(settings.EnabledPlugins)))
	}
	fmt.Printf("  [OK] %s (name=%s): %s\n", secretName, settings.Name, strings.Join(parts, ", "))
	if migrateVerbose {
		printVerboseSettings(settings)
	}
}

func namedCount(name string, values []string) string {
	if len(values) == 0 {
		return name + "=0"
	}
	return fmt.Sprintf("%s=%d(%s)", name, len(values), strings.Join(values, ","))
}

func mcpServerNames(servers map[string]*migrateMCPServerJSON) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	return names
}

func marketplaceNames(marketplaces map[string]*migrateMarketplaceJSON) []string {
	names := make([]string, 0, len(marketplaces))
	for name := range marketplaces {
		names = append(names, name)
	}
	return names
}

func printVerboseSettings(settings migrateSettingsJSON) {
	for name, server := range settings.MCPServers {
		fmt.Printf("       MCP: %s (type=%s", name, server.Type)
		if server.URL != "" {
			fmt.Printf(", url=%s", server.URL)
		}
		if server.Command != "" {
			fmt.Printf(", cmd=%s", server.Command)
		}
		fmt.Println(")")
	}
	if len(settings.EnvVars) > 0 {
		fmt.Printf("       EnvVars: %s\n", strings.Join(mapKeys(settings.EnvVars), ", "))
	}
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func printCleanupSkipped() {
	fmt.Println("\nPhase 2 (cleanup) skipped.")
	fmt.Println("Run with --cleanup to delete derived Secrets.")
	fmt.Println("Run with --cleanup --dry-run to preview what would be deleted.")
}

func cleanupDerivedSecrets(ctx context.Context, client kubernetes.Interface) error {
	fmt.Println("\n=== Phase 2: Cleanup derived Secrets ===")
	if migrateDryRun {
		fmt.Println("[DRY-RUN] The following Secrets would be deleted:")
	}
	derivedLabelSelectors := []struct {
		typeLabel   string
		description string
	}{
		{"agentapi.proxy/mcp-servers=true", "MCP servers secrets (mcp-servers-*)"},
		{"agentapi.proxy/marketplaces=true", "Marketplace secrets (marketplaces-*)"},
		{"agentapi.proxy/env=true", "Environment variable secrets (agent-env-*)"},
	}
	total := 0
	for _, derived := range derivedLabelSelectors {
		count, err := cleanupDerivedSecretType(ctx, client, derived.typeLabel, derived.description)
		if err != nil {
			return err
		}
		total += count
	}
	if migrateDryRun {
		fmt.Printf("\nDry-run complete: %d secret(s) would be deleted.\n", total)
		fmt.Println("Run without --dry-run to actually delete them.")
	} else {
		fmt.Printf("\nCleanup complete: %d secret(s) deleted.\n", total)
	}
	return nil
}

func cleanupDerivedSecretType(ctx context.Context, client kubernetes.Interface, typeLabel, description string) (int, error) {
	selector := "agentapi.proxy/managed-by=settings," + typeLabel
	secrets, err := client.CoreV1().Secrets(migrateNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, fmt.Errorf("failed to list %s: %w", description, err)
	}
	if len(secrets.Items) == 0 {
		if migrateVerbose {
			fmt.Printf("  No %s found\n", description)
		}
		return 0, nil
	}
	fmt.Printf("  %s: %d secret(s) found\n", description, len(secrets.Items))
	count := 0
	for _, secret := range secrets.Items {
		if migrateDryRun {
			fmt.Printf("    [DRY-RUN] Would delete: %s\n", secret.Name)
			count++
			continue
		}
		if err := client.CoreV1().Secrets(migrateNamespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("    [ERROR] Failed to delete %s: %v\n", secret.Name, err)
			continue
		}
		fmt.Printf("    [DELETED] %s\n", secret.Name)
		count++
	}
	return count, nil
}
