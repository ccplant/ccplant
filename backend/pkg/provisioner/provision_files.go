package provisioner

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) applyProvisionRestartSettings(settings *sessionsettings.SessionSettings, compileOptions sessionsettings.CompileOptions) bool {
	if !settings.Restart {
		return true
	}
	for _, path := range settings.RemoveFiles {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.setStatus(StatusError, "failed to remove obsolete managed file")
			return false
		}
	}
	settings.RemoveFiles = nil
	if err := cleanRestartFiles(settings, compileOptions.OutputDir); err != nil {
		s.setStatus(StatusError, "failed to remove obsolete settings")
		return false
	}
	if err := sessionsettings.CompileSettings(settings, compileOptions); err != nil {
		s.setStatus(StatusError, "failed to apply restart settings")
		return false
	}
	return true
}

func (s *Server) restoreProvisionManagedFiles(settings *sessionsettings.SessionSettings, outputDirectory string) bool {
	if len(settings.Files) == 0 {
		restoreLegacyProvisionCredentials(settings.Credentials)
		return true
	}
	if err := writeFiles(settings.Files); err != nil {
		if settings.Restart {
			s.setStatus(StatusError, "failed to apply managed files")
			return false
		}
		log.Printf("[PROVISIONER] Warning: failed to write managed files: %v", err)
	}
	if err := restoreAntigravityAuthBundle(settings.Files); err != nil {
		if settings.Restart {
			s.setStatus(StatusError, "failed to restore Antigravity authentication")
			return false
		}
		log.Printf("[PROVISIONER] Warning: failed to restore Antigravity authentication: %v", err)
	}
	if settings.Claude.SettingsJSON != nil {
		settingsPath := filepath.Join(outputDirectory, ".claude", "settings.json")
		if err := mergeHooksIntoSettingsFile(settingsPath, settings.Claude.SettingsJSON); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to re-apply hooks to settings.json: %v", err)
		}
	}
	return true
}

func restoreLegacyProvisionCredentials(credentials string) {
	if credentials == "" {
		return
	}
	if err := writeCredentials(credentials); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write credentials (legacy): %v", err)
		return
	}
	log.Printf("[PROVISIONER] Restored credentials to ~/.codex/auth.json (legacy)")
}

func (s *Server) finishProvisionFileRestore(ctx context.Context, settings *sessionsettings.SessionSettings, outputDirectory string, preserveConnection bool) bool {
	managedStorage := false
	if err := s.restoreObjectStoragesMatching(ctx, settings, &managedStorage); err != nil {
		s.setStatus(StatusError, "failed to restore persistent storage: "+err.Error())
		return false
	}
	if err := persistModelConnectionIdentity(settings, outputDirectory, preserveConnection); err != nil {
		s.setStatus(StatusError, err.Error())
		return false
	}
	if err := cleanModelConnectionFiles(settings, outputDirectory); err != nil {
		s.setStatus(StatusError, "failed to prepare connection files: "+err.Error())
		return false
	}
	if err := writeCodexRequirements(settings.Codex.HooksJSON); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Codex managed requirements: %v", err)
	}
	return true
}
