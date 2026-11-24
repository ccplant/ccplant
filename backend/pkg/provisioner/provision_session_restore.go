package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type provisionRestoreState struct {
	source   string
	required bool
}

type provisionRestoreOutcome struct {
	connectionState bool
	marked          bool
}

func (s *Server) prepareProvisionSession(ctx context.Context, settings *sessionsettings.SessionSettings) (sessionsettings.CompileOptions, provisionRestoreState, bool) {
	compileOptions := sessionsettings.DefaultCompileOptions()
	if err := confirmRuntimeStart(ctx, settings.ParentRuntime); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("runtime start rejected: %v", err))
		return compileOptions, provisionRestoreState{}, false
	}
	s.setPhase("provision:wait-mounted-settings")
	if err := waitForMountedSettingsSanitized(ctx, s.settingsFile, mountedSettingsSanitizeTimeout); err != nil {
		s.setStatus(StatusError, "mounted session settings were not sanitized: "+err.Error())
		return compileOptions, provisionRestoreState{}, false
	}
	s.setPhase("provision:write-webhook-payload")
	if settings.WebhookPayload != "" {
		writeWebhookPayloadFile(settings.WebhookPayload)
	}
	managedStorage := true
	if err := s.restoreObjectStoragesMatching(ctx, settings, &managedStorage); err != nil {
		s.setStatus(StatusError, "failed to restore managed session state: "+err.Error())
		return compileOptions, provisionRestoreState{}, false
	}
	restoreState := provisionRestoreState{source: settings.Session.ResumeFrom}
	restoreState.required = restoreState.source != "" || settings.Restart
	if restoreState.source == "" && shouldImplicitlyRestoreSessionState(settings) {
		restoreState.source = settings.Session.ID
	}
	settings.WorkspaceKept = restoreState.source != "" && workspaceAlreadyRestored(restoreState.source)
	setupOptions := sessionsettings.SetupOptions{
		CompileOptions:            compileOptions,
		NotificationSubscriptions: nativeRuntimePath("notification-subscriptions-source", "/notification-subscriptions-source"),
		NotificationsDir:          filepath.Join(runtimeHome, "notifications"),
		RegisterMarketplaces:      true,
	}
	s.setPhase("provision:session-setup")
	log.Printf("[PROVISIONER] Running session setup")
	if err := sessionsettings.SetupSettings(settings, setupOptions); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("setup failed: %v", err))
		return compileOptions, restoreState, false
	}
	log.Printf("[PROVISIONER] Session setup complete")
	return compileOptions, restoreState, true
}

func (s *Server) restoreProvisionSession(ctx context.Context, settings *sessionsettings.SessionSettings, state provisionRestoreState, markerPath string) (provisionRestoreOutcome, bool) {
	outcome := provisionRestoreOutcome{}
	restoreCompleted := true
	if state.source != "" && !settings.RestartInPlace {
		var err error
		outcome.connectionState, restoreCompleted, err = s.restoreProvisionSource(ctx, settings, state)
		if err != nil {
			s.setStatus(StatusError, err.Error())
			return outcome, false
		}
	}
	if !restoreCompleted {
		return outcome, true
	}
	if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		s.setStatus(StatusError, fmt.Sprintf("failed to record session restore completion: %v", err))
		return outcome, false
	}
	outcome.marked = true
	return outcome, true
}

func (s *Server) restoreProvisionSource(ctx context.Context, settings *sessionsettings.SessionSettings, state provisionRestoreState) (bool, bool, error) {
	s.setPhase("provision:restore-session-state")
	connectionPath := filepath.Join(runtimeHome, ".session", "model-connection.json")
	if err := os.Remove(connectionPath); err != nil && !os.IsNotExist(err) {
		return false, false, errors.New("failed to prepare restored connection identity")
	}
	restoreDirectory := filepath.Dir(workdirRepoPath)
	if settings.Repository != nil && strings.TrimSpace(settings.Repository.FullName) != "" {
		restoreDirectory = workdirRepoPath
	}
	found, err := s.restoreSessionState(ctx, state.source, restoreDirectory, settings.WorkspaceKept)
	if errors.Is(err, errSessionStateBackendUnavailable) && !settings.Restart {
		log.Printf("[PROVISIONER] Session state restore skipped; continuing without persisted state: %v", err)
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("session state restore failed: %w", err)
	}
	if !found && state.required {
		return false, false, fmt.Errorf("session state restore failed: snapshot %s was not found", state.source)
	}
	if !found {
		return false, true, nil
	}
	log.Printf("[PROVISIONER] Restored persisted ACP state for proxy session %s from %s", settings.Session.ID, state.source)
	if err := recordWorkspaceRestored(state.source); err != nil {
		return false, false, fmt.Errorf("failed to record workspace restore: %w", err)
	}
	return true, true, nil
}
