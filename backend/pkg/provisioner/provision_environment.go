package provisioner

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) prepareProvisionEnvironment(ctx context.Context, settings *sessionsettings.SessionSettings, outputDirectory string) (map[string]string, func(), bool) {
	s.setPhase("provision:load-env")
	environment := cloneEnvironment(settings.Env)
	if settings.Session.AgentType == "antigravity" && managedFilePresent(settings.Files, sessionsettings.FileTypeGoogleADC) {
		environment["AGY_ADC_AUTH"] = "true"
	}
	if strings.TrimSpace(os.Getenv("CCPLANT_EXECUTOR_REQUIRED")) == "1" && len(settings.Files) > 0 {
		paths := make([]string, 0, len(settings.Files))
		for _, file := range settings.Files {
			paths = append(paths, file.Path)
		}
		encoded, err := json.Marshal(paths)
		if err != nil {
			s.setStatus(StatusError, "failed to prepare executor managed files")
			return nil, nil, false
		}
		environment["CCPLANT_EXECUTOR_MANAGED_FILE_PATHS"] = string(encoded)
	}
	log.Printf("[PROVISIONER] Prepared %d in-memory env vars", len(environment))
	prepareSciaCABundle(ctx, environment)
	stopEndpoint, err := prepareModelEndpoint(settings, outputDirectory, environment)
	if err != nil {
		s.setStatus(StatusError, err.Error())
		return nil, nil, false
	}
	return environment, stopEndpoint, true
}

func (s *Server) prepareProvisionWorkspace(ctx context.Context, settings *sessionsettings.SessionSettings, environment map[string]string) {
	s.setPhase("provision:fetch-memory")
	s.setPhase("provision:write-agent-files")
	if err := writePiAgentInstructions(claudeMDPath, piAgentInstructionsPath); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Pi AGENTS.md: %v", err)
	}
	if err := writePiSettings(piSettingsPath, piMCPServers(settings)); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Pi settings: %v", err)
	}
	if err := copyAgentInstructions(claudeMDPath, antigravityAgentInstructionsPath, "Antigravity"); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Antigravity AGENTS.md: %v", err)
	}
	if err := writeAntigravityMCPConfig(antigravityMCPConfigPath, piMCPServers(settings)); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Antigravity MCP config: %v", err)
	}
	s.prepareProvisionWorkingDirectory()
	if settings.Startup.PreScript != "" && !settings.Restart {
		s.setPhase("provision:pre-script")
		log.Printf("[PROVISIONER] Running pre-script")
		if err := s.runPreScript(ctx, settings.Startup.PreScript, environment); err != nil {
			log.Printf("[PROVISIONER] Warning: pre-script failed (continuing): %v", err)
		} else {
			log.Printf("[PROVISIONER] Pre-script complete")
		}
	}
	if settings.OtelCollector != nil && settings.OtelCollector.Enabled {
		s.agentWorkers.Add(1)
		go func() { defer s.agentWorkers.Done(); s.runOtelcol(ctx, settings.OtelCollector) }()
	}
}

func (s *Server) prepareProvisionWorkingDirectory() {
	s.setPhase("provision:chdir")
	if _, err := os.Stat(workdirRepoPath); err == nil {
		log.Printf("[PROVISIONER] Changing to repo directory %s", workdirRepoPath)
		if err := os.Chdir(workdirRepoPath); err != nil {
			log.Printf("[PROVISIONER] Warning: failed to chdir to %s: %v", workdirRepoPath, err)
		}
	}
	if os.Getenv("AGENTAPI_NATIVE_SESSION_ROOT") != "" {
		if cwd, err := os.Getwd(); err == nil {
			if err := trustNativeWorkspace(filepath.Join(runtimeHome, ".claude.json"), cwd); err != nil {
				log.Printf("[PROVISIONER] Warning: failed to mark native workspace trusted: %v", err)
			}
		}
	}
	if cwd, err := os.Getwd(); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to resolve Antigravity workspace: %v", err)
	} else if err := writeAntigravitySettings(antigravitySettingsPath, cwd); err != nil {
		log.Printf("[PROVISIONER] Warning: failed to write Antigravity settings: %v", err)
	}
}
