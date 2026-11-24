package startup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func installPlugin(outputDir string, pluginIdentifier string) error {
	log.Printf("[SYNC] Installing plugin: %s", pluginIdentifier)
	if err := runClaudeCLI(outputDir, "plugin", "install", pluginIdentifier); err != nil {
		return err
	}
	log.Printf("[SYNC] Successfully installed plugin: %s", pluginIdentifier)
	return nil
}

// syncCredentials copies credentials.json from the mounted Secret to
// ~/.claude/.credentials.json (Claude Code CLI credentials).
func syncCredentials(credentialsFile, outputDir string) error {
	if _, err := os.Stat(credentialsFile); os.IsNotExist(err) {
		log.Printf("[SYNC] Credentials file not found at %s, skipping", credentialsFile)
		return nil
	}

	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return fmt.Errorf("failed to read credentials file: %w", err)
	}

	if !json.Valid(data) {
		return fmt.Errorf("credentials file is not valid JSON")
	}

	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	destPath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(destPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	log.Printf("[SYNC] Copied credentials to %s", destPath)
	return nil
}

// SyncExtra copies credentials and notification subscriptions.
// Config files (.claude.json / settings.json) are handled by sessionsettings.Compile.
// CLAUDE.md is not copied here because the same content is provided via /etc/claude-code/CLAUDE.md.
func SyncExtra(opts SyncOptions) error {
	if opts.CredentialsFile != "" {
		if err := syncCredentials(opts.CredentialsFile, opts.OutputDir); err != nil {
			log.Printf("[SYNC-EXTRA] Warning: failed to sync credentials: %v", err)
		}
	}

	if opts.NotificationSubscriptions != "" && opts.NotificationsDir != "" {
		if err := syncNotificationSubscriptions(opts.NotificationSubscriptions, opts.NotificationsDir); err != nil {
			log.Printf("[SYNC-EXTRA] Warning: failed to sync notification subscriptions: %v", err)
		}
	}

	return nil
}

// syncNotificationSubscriptions copies notification subscription files from a mounted
// Secret directory to the notifications output directory, following symlinks.
func syncNotificationSubscriptions(subscriptionsDir, notificationsDir string) error {
	if _, err := os.Stat(subscriptionsDir); os.IsNotExist(err) {
		log.Printf("[SYNC] Notification subscriptions directory not found at %s, skipping", subscriptionsDir)
		return nil
	}

	if err := os.MkdirAll(notificationsDir, 0755); err != nil {
		return fmt.Errorf("failed to create notifications directory: %w", err)
	}

	entries, err := os.ReadDir(subscriptionsDir)
	if err != nil {
		return fmt.Errorf("failed to read subscriptions directory: %w", err)
	}

	if len(entries) == 0 {
		log.Printf("[SYNC] No notification subscriptions found, skipping")
		return nil
	}

	copiedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(subscriptionsDir, entry.Name())
		destPath := filepath.Join(notificationsDir, entry.Name())

		data, err := os.ReadFile(srcPath)
		if err != nil {
			log.Printf("[SYNC] Warning: failed to read subscription file %s: %v", srcPath, err)
			continue
		}

		if err := os.WriteFile(destPath, data, 0644); err != nil {
			log.Printf("[SYNC] Warning: failed to write subscription file %s: %v", destPath, err)
			continue
		}

		copiedCount++
	}

	log.Printf("[SYNC] Copied %d notification subscriptions to %s", copiedCount, notificationsDir)
	return nil
}

// syncCodexSkills copies skill directories from installed marketplace plugins into
// ~/.codex/skills/<skill-name>/ so the Codex CLI discovers them natively.
//
// Codex reads skills from ~/.codex/skills/<skill-name>/SKILL.md (user scope) using
// the same SKILL.md format as Claude Code marketplace plugins, so no transformation
// is needed — the directory is copied as-is.
//
// The function is idempotent: each run overwrites previously synced skill directories.
//
// Directory layout expected:
//
//	<marketplacesDir>/<marketplace>/plugins/<plugin>/skills/<skill-name>/SKILL.md
func syncCodexSkills(outputDir, marketplacesDir string, enabledPlugins []string) error {
	codexSkillsDir := filepath.Join(outputDir, ".codex", "skills")
	enabled := make(map[string]struct{}, len(enabledPlugins))
	for _, plugin := range enabledPlugins {
		enabled[plugin] = struct{}{}
	}

	marketplaceEntries, err := os.ReadDir(marketplacesDir)
	if err != nil {
		log.Printf("[SYNC] syncCodexSkills: no marketplaces dir at %s, skipping", marketplacesDir)
		return nil
	}

	copiedCount := 0
	for _, mEntry := range marketplaceEntries {
		if !mEntry.IsDir() || strings.HasPrefix(mEntry.Name(), ".tmp-") {
			continue
		}

		pluginsDir := filepath.Join(marketplacesDir, mEntry.Name(), "plugins")
		pluginEntries, err := os.ReadDir(pluginsDir)
		if err != nil {
			continue
		}

		for _, pEntry := range pluginEntries {
			if !pEntry.IsDir() {
				continue
			}
			pluginID := pEntry.Name() + "@" + mEntry.Name()
			if _, ok := enabled[pluginID]; !ok {
				continue
			}

			skillsDir := filepath.Join(pluginsDir, pEntry.Name(), "skills")
			skillEntries, err := os.ReadDir(skillsDir)
			if err != nil {
				continue
			}

			for _, sEntry := range skillEntries {
				if !sEntry.IsDir() {
					continue
				}

				srcDir := filepath.Join(skillsDir, sEntry.Name())
				destDir := filepath.Join(codexSkillsDir, sEntry.Name())

				if err := copySkillDir(srcDir, destDir); err != nil {
					log.Printf("[SYNC] Warning: failed to copy skill %s to Codex: %v", sEntry.Name(), err)
					continue
				}
				copiedCount++
				log.Printf("[SYNC] Copied skill %s to %s", sEntry.Name(), destDir)
			}
		}
	}

	if copiedCount == 0 {
		log.Printf("[SYNC] No skills found in marketplaces, skipping Codex skills sync")
		return nil
	}

	log.Printf("[SYNC] Synced %d skill(s) from marketplaces to %s", copiedCount, codexSkillsDir)
	return nil
}

// copySkillDir recursively copies a complete skill bundle. Symlinks and other
// non-regular entries are rejected so a marketplace cannot escape its bundle.
func copySkillDir(srcDir, destDir string) error {
	return filepath.WalkDir(srcDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("unsupported skill bundle entry: %s", rel)
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", rel, err)
		}
		mode := info.Mode().Perm()
		if mode&0111 == 0 {
			mode = 0644
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("failed to write %s: %w", rel, err)
		}
		return nil
	})
}
