package startup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// SyncOptions contains options for the sync command
type SyncOptions struct {
	SettingsFile              string // Path to the mounted settings.json from Settings Secret
	OutputDir                 string // Home directory (generates ~/.claude.json and ~/.claude/)
	CredentialsFile           string // Path to the mounted credentials.json from Credentials Secret (optional)
	NotificationSubscriptions string // Path to notification subscriptions directory (optional)
	NotificationsDir          string // Path to notifications output directory (optional)
	RegisterMarketplaces      bool   // Register cloned marketplaces using claude CLI
	AgentType                 string // Resolved runtime agent type (claude-acp, codex-acp, ...)
}

// settingsJSON represents the structure of settings.json from Settings Secret
type settingsJSON struct {
	Name           string                      `json:"name"`
	Bedrock        *bedrockJSON                `json:"bedrock,omitempty"`
	MCPServers     map[string]*mcpServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	EnabledPlugins []string                    `json:"enabled_plugins,omitempty"` // plugin@marketplace format
	Skills         []string                    `json:"skills,omitempty"`          // skills.sh package sources
	Hooks          map[string]interface{}      `json:"hooks,omitempty"`
	CreatedAt      string                      `json:"created_at"`
	UpdatedAt      string                      `json:"updated_at"`
}

// bedrockJSON represents Bedrock settings (not used in sync, but part of structure)
type bedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// mcpServerJSON represents MCP server settings (not used in sync, but part of structure)
type mcpServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// marketplaceJSON represents a marketplace configuration
type marketplaceJSON struct {
	URL string `json:"url"`
}

// marketplacePluginJSON represents .claude-plugin/marketplace.json in a marketplace repository
type marketplacePluginJSON struct {
	Name string `json:"name"`
}

// Sync synchronizes settings from Settings Secret to Claude configuration files.
// It assumes ~/.claude.json and ~/.claude/settings.json have already been written
// by the compile step; it handles marketplace clone/register and plugin install only.
func Sync(opts SyncOptions) error {
	log.Printf("[SYNC] Starting sync: output dir: %s, register marketplaces: %v",
		opts.OutputDir, opts.RegisterMarketplaces)

	settings, err := loadSettingsFile(opts.SettingsFile)
	if err != nil {
		settings = nil
	}

	if err := syncMarketplaces(opts, settings); err != nil {
		return fmt.Errorf("failed to sync marketplaces: %w", err)
	}

	claudeSettings, err := readClaudeSettingsJSON(filepath.Join(opts.OutputDir, ".claude", "settings.json"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read skills settings: %w", err)
	}
	if claudeSettings != nil && len(claudeSettings.Skills) > 0 {
		agent, err := resolveSkillsAgent(opts.AgentType)
		if err != nil {
			return err
		}
		if agent == "" {
			log.Printf("[SYNC] Skills packages are not supported for agent type %s; skipping", opts.AgentType)
		} else if err := installSkillsPackages(opts.OutputDir, agent, claudeSettings.Skills); err != nil {
			return err
		} else if agent == "antigravity-cli" {
			if err := syncAntigravitySkills(opts.OutputDir); err != nil {
				return err
			}
		}
	}

	if opts.CredentialsFile != "" {
		if err := syncCredentials(opts.CredentialsFile, opts.OutputDir); err != nil {
			log.Printf("[SYNC] Warning: failed to sync credentials: %v", err)
		}
	}

	if opts.NotificationSubscriptions != "" && opts.NotificationsDir != "" {
		if err := syncNotificationSubscriptions(opts.NotificationSubscriptions, opts.NotificationsDir); err != nil {
			log.Printf("[SYNC] Warning: failed to sync notification subscriptions: %v", err)
		}
	}

	log.Printf("[SYNC] Sync completed successfully")
	return nil
}

// loadSettingsFile loads settings from the mounted Settings Secret
func loadSettingsFile(settingsFile string) (*settingsJSON, error) {
	if settingsFile == "" {
		return nil, fmt.Errorf("settings file path is empty")
	}

	data, err := os.ReadFile(settingsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings settingsJSON
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings JSON: %w", err)
	}

	log.Printf("[SYNC] Loaded settings for user: %s", settings.Name)
	return &settings, nil
}

// generateClaudeJSON generates ~/.claude.json with onboarding settings,
// preserving any existing fields.
func generateClaudeJSON(outputDir string) error {
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")

	existing := make(map[string]interface{})
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			log.Printf("[SYNC] Warning: failed to parse existing .claude.json: %v", err)
			existing = make(map[string]interface{})
		}
	}

	existing["hasCompletedOnboarding"] = true
	existing["bypassPermissionsModeAccepted"] = true

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}

	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	log.Printf("[SYNC] Generated %s", claudeJSONPath)
	return nil
}

// syncMarketplaces clones custom marketplace repositories and registers marketplaces
// and plugins via claude CLI. settings.json is assumed to already be written by
// the compile step; this function does NOT overwrite it.
func syncMarketplaces(opts SyncOptions, settings *settingsJSON) error {
	claudeDir := filepath.Join(opts.OutputDir, ".claude")
	marketplacesDir := filepath.Join(claudeDir, "plugins", "marketplaces")
	if err := os.MkdirAll(marketplacesDir, 0755); err != nil {
		return fmt.Errorf("failed to create marketplaces directory: %w", err)
	}

	nameMapping := make(map[string]string)

	// Read marketplace config from compile-generated ~/.claude/settings.json
	settingsPath := filepath.Join(claudeDir, "settings.json")
	compileSettings, err := readClaudeSettingsJSON(settingsPath)
	if err != nil {
		log.Printf("[SYNC] Warning: could not read %s: %v", settingsPath, err)
		compileSettings = &claudeSettingsJSON{}
	}

	// Merge marketplaces: compile-generated settings take precedence,
	// external settings provide additional custom marketplace URLs.
	mergedMarketplaces := make(map[string]*marketplaceJSON)
	for k, v := range compileSettings.Marketplaces {
		mergedMarketplaces[k] = v
	}
	if settings != nil {
		for k, v := range settings.Marketplaces {
			if _, exists := mergedMarketplaces[k]; !exists {
				mergedMarketplaces[k] = v
			}
		}
	}

	// Clone custom marketplace repositories
	for aliasKey, marketplace := range mergedMarketplaces {
		if marketplace.URL == "" {
			log.Printf("[SYNC] Skipping marketplace %s: no URL configured", aliasKey)
			continue
		}

		tempDir := filepath.Join(marketplacesDir, ".tmp-"+aliasKey)
		log.Printf("[SYNC] Cloning marketplace %s from %s", aliasKey, marketplace.URL)

		if err := cloneMarketplace(marketplace.URL, tempDir); err != nil {
			log.Printf("[SYNC] Warning: failed to clone marketplace %s: %v", aliasKey, err)
			continue
		}

		realName, err := readMarketplaceName(tempDir)
		if err != nil {
			log.Printf("[SYNC] Error: failed to read marketplace name for %s: %v", aliasKey, err)
			removeTempDir(tempDir)
			continue
		}

		targetDir := filepath.Join(marketplacesDir, realName)
		if err := os.Rename(tempDir, targetDir); err != nil {
			log.Printf("[SYNC] Error: failed to rename marketplace dir from %s to %s: %v", tempDir, targetDir, err)
			removeTempDir(tempDir)
			continue
		}

		nameMapping[aliasKey] = realName
		log.Printf("[SYNC] Cloned marketplace %s (alias: %s) at %s", realName, aliasKey, targetDir)
	}

	if !opts.RegisterMarketplaces {
		return nil
	}

	agent, err := resolveSkillsAgent(opts.AgentType)
	if err != nil {
		return err
	}
	if agent == "codex" {
		// Codex consumes portable SKILL.md bundles directly. Do not register or
		// activate Claude plugins in a Codex session.
		resolvedPlugins := make([]string, 0, len(compileSettings.EnabledPlugins))
		for _, plugin := range compileSettings.EnabledPlugins {
			resolvedPlugins = append(resolvedPlugins, resolvePluginName(plugin, nameMapping))
		}
		if hasMarketplacePlugin(resolvedPlugins, "claude-plugins-official") {
			if err := ensureOfficialMarketplaceClone(marketplacesDir); err != nil {
				return fmt.Errorf("clone official marketplace for Codex: %w", err)
			}
		}
		if err := syncCodexSkills(opts.OutputDir, marketplacesDir, resolvedPlugins); err != nil {
			return fmt.Errorf("sync Codex skills: %w", err)
		}
		return nil
	}
	if agent == "" {
		// The selected agent has no defined skills integration. In particular,
		// Pi must not inherit Codex skills implicitly.
		return nil
	}

	if err := registerOfficialMarketplace(opts.OutputDir); err != nil {
		log.Printf("[SYNC] Warning: failed to register official marketplace: %v", err)
	}

	if err := registerMarketplaces(opts.OutputDir, marketplacesDir); err != nil {
		log.Printf("[SYNC] Warning: failed to register marketplaces: %v", err)
	}

	// Install enabled plugins from the compile-generated settings.json
	for _, plugin := range compileSettings.EnabledPlugins {
		resolvedPlugin := resolvePluginName(plugin, nameMapping)
		if err := installPlugin(opts.OutputDir, resolvedPlugin); err != nil {
			log.Printf("[SYNC] Warning: failed to install plugin %s: %v", resolvedPlugin, err)
		}
	}

	return nil
}

func hasMarketplacePlugin(plugins []string, marketplace string) bool {
	for _, plugin := range plugins {
		if strings.HasSuffix(plugin, "@"+marketplace) {
			return true
		}
	}
	return false
}

func ensureOfficialMarketplaceClone(marketplacesDir string) error {
	targetDir := filepath.Join(marketplacesDir, "claude-plugins-official")
	return cloneMarketplace("https://github.com/anthropics/claude-plugins-official.git", targetDir)
}

// resolveSkillsAgent maps the already-resolved runtime agent type to the
// canonical skills.sh agent identifier. Empty is accepted for the standalone
// sync CLI and preserves its historical Claude behavior.
func resolveSkillsAgent(agentType string) (string, error) {
	switch strings.TrimSpace(agentType) {
	case "", "claude", "claude-acp", "claude-legacy":
		return "claude-code", nil
	case "codex", "codex-acp":
		return "codex", nil
	case "antigravity":
		return "antigravity-cli", nil
	case "pi", "cursor":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported agent type for skills: %s", agentType)
	}
}

// syncAntigravitySkills handles older skills CLI releases that install global
// Antigravity CLI skills into the universal ~/.agents/skills directory. Keep
// that compatible copy while also populating Antigravity's documented global
// discovery directory.
func syncAntigravitySkills(outputDir string) error {
	sourceRoot := filepath.Join(outputDir, ".agents", "skills")
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Antigravity skills source: %w", err)
	}
	destinationRoot := filepath.Join(outputDir, ".gemini", "antigravity-cli", "skills")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(sourceRoot, entry.Name())
		if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect Antigravity skill %s: %w", entry.Name(), err)
		}
		if err := copySkillDir(source, filepath.Join(destinationRoot, entry.Name())); err != nil {
			return fmt.Errorf("copy Antigravity skill %s: %w", entry.Name(), err)
		}
	}
	log.Printf("[SYNC] Synchronized Antigravity CLI skills to %s", destinationRoot)
	return nil
}

// removeTempDir removes a temporary directory, logging a warning on failure.
func removeTempDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("[SYNC] Warning: failed to remove temp dir %s: %v", dir, err)
	}
}

// claudeSettingsJSON represents the subset of ~/.claude/settings.json we care about
