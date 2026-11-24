package provisioner

import (
	"context"
	"log"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func (s *Server) runProvision(parent context.Context, settings *sessionsettings.SessionSettings) {
	run, ready := s.beginProvisionRun(parent, settings)
	if !ready {
		return
	}
	defer run.finish()
	if settings.Paused {
		s.setStatus(Status("paused"), "")
		return
	}
	normalizeNativeSettings(settings)
	injectUsageReportingHook(settings)
	s.setPhase("provision:start")
	log.Printf("[PROVISIONER] Starting provisioning for session %s", settings.Session.ID)
	defer run.logCancellation()

	compileOpts, restoreState, setupReady := s.prepareProvisionSession(run.ctx, settings)
	if !setupReady {
		return
	}
	restoreRequired := restoreState.required
	restoreOutcome, restoreReady := s.restoreProvisionSession(run.ctx, settings, restoreState, run.markerPath)
	if !restoreReady {
		return
	}
	restoredConnectionState := restoreOutcome.connectionState
	run.restoreMarked = restoreOutcome.marked

	if !s.applyProvisionRestartSettings(settings, compileOpts) {
		return
	}
	// ── Step 2.3: docker login for DinD registries ───────────────────────────
	s.setPhase("provision:post-setup")
	if settings.Docker != nil && settings.Docker.Enabled {
		s.agentWorkers.Add(1)
		go func() { defer s.agentWorkers.Done(); s.runDockerLogins(run.ctx, settings.Docker) }()
	}

	if !s.restoreProvisionManagedFiles(settings, compileOpts.OutputDir) {
		return
	}
	if !s.finishProvisionFileRestore(run.ctx, settings, compileOpts.OutputDir, restoreRequired || restoredConnectionState) {
		return
	}

	envMap, stopEndpoint, environmentReady := s.prepareProvisionEnvironment(run.ctx, settings, compileOpts.OutputDir)
	if !environmentReady {
		return
	}
	defer stopEndpoint()
	s.prepareProvisionWorkspace(run.ctx, settings, envMap)

	run.processStarted = s.runAgentLifecycle(run.ctx, settings, envMap, run.exited)
}

func (s *Server) runAgentLifecycle(ctx context.Context, settings *sessionsettings.SessionSettings, envMap map[string]string, exited chan struct{}) bool {
	s.setPhase("provision:start-agent")
	agent, err := s.startProvisionedAgent(ctx, settings, envMap, exited)
	if err != nil {
		s.setStatus(StatusError, err.Error())
		return false
	}

	if !s.waitForProvisionedAgent(ctx, agent) {
		return true
	}

	// Enable the network-filter policy now that the agent is fully started.
	// This is a best-effort call; if there is no sandbox sidecar the request
	// simply fails and is ignored.
	configureNetworkFilterPolicy(settings.Sandbox)
	enableNetworkFilterPolicy()

	// ── Step 9: send initial message ─────────────────────────────────────────
	s.sendProvisionInitialMessage(ctx, settings)

	// ── Step 10: mark ready and supervise ────────────────────────────────────
	s.setPhase("provision:ready")
	s.setStatus(StatusReady, "")
	if err := scheduleSessionSuspendFromReady(ctx); err != nil {
		log.Printf("[PROVISIONER] Failed to schedule idle suspend after setup for session %s: %v", settings.Session.ID, err)
	}

	// ── Step 11: launch acp-posts subprocess if SlackParams provided ─────────
	if settings.SlackParams != nil && settings.SlackParams.Channel != "" {
		s.agentWorkers.Add(1)
		go func() {
			defer s.agentWorkers.Done()
			s.runAcpPosts(ctx, settings.SlackParams, settings.Session.AgentType)
		}()
	}

	// ── Step 12: start files sync goroutine ───────────────────────────────────
	// Syncs managedFilePaths through the authenticated session control API.
	// Runs in-process instead of as a sidecar so that UserID is always set
	// (stock pool pods have empty UserID at pod creation time).
	s.agentWorkers.Add(1)
	go func() {
		defer s.agentWorkers.Done()
		s.runFilesSync(ctx, settings)
	}()
	s.runStorageSync(ctx, settings)

	// Supervise: if agentapi exits, report error so K8s restarts the Pod.
	go s.superviseProvisionedAgent(ctx, agent.done)
	return true
}
