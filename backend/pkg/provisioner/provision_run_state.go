package provisioner

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type provisionRunState struct {
	server         *Server
	settings       *sessionsettings.SessionSettings
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	exited         chan struct{}
	markerPath     string
	abortedPath    string
	startedAt      time.Time
	processStarted bool
	restoreMarked  bool
}

func (s *Server) beginProvisionRun(parent context.Context, settings *sessionsettings.SessionSettings) (*provisionRunState, bool) {
	if settings.Env == nil {
		settings.Env = make(map[string]string)
	}
	s.configureCLIBrokerForSession(settings)
	delete(settings.Env, "AGENTAPI_KEY")
	settings.Env["AGENTAPI_PROXY_ENDPOINT"] = "http+unix://%2Ftmp%2Fccplant%2Fprovisioner.sock"
	run := &provisionRunState{
		server: s, settings: settings,
		markerPath:  sessionRestoreMarkerPath(settings.Session.ID),
		abortedPath: sessionProvisionAbortedMarkerPath(settings.Session.ID),
		startedAt:   time.Now(),
	}
	for _, path := range []string{run.markerPath, run.abortedPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.setStatus(StatusError, fmt.Sprintf("failed to reset session restore marker: %v", err))
			return nil, false
		}
	}
	run.ctx, run.cancel = context.WithCancel(parent)
	s.mu.Lock()
	s.agentCancel = run.cancel
	s.activeSettings = settings
	s.restartID = settings.RestartID
	s.provisionDone = make(chan struct{})
	run.done = s.provisionDone
	s.agentExited = make(chan struct{})
	run.exited = s.agentExited
	s.mu.Unlock()
	return run, true
}

func (r *provisionRunState) finish() {
	close(r.done)
	if !r.processStarted {
		r.cancel()
		close(r.exited)
	}
	if r.processStarted || r.restoreMarked || r.settings.RestartInPlace || r.settings.Paused {
		return
	}
	contents := []byte(time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(r.abortedPath, contents, 0o600); err != nil {
		log.Printf("[PROVISIONER] Failed to record aborted provisioning for session %s: %v", r.settings.Session.ID, err)
	}
}

func (r *provisionRunState) logCancellation() {
	if r.ctx.Err() == nil {
		return
	}
	status, message, phase, elapsed := r.server.snapshot()
	log.Printf(
		"[PROVISIONER] Provisioning stopped because context was cancelled (session=%s, status=%s, message=%q, phase=%q, phase_elapsed=%s, total_elapsed=%s, err=%v)",
		r.settings.Session.ID, status, message, phase, elapsed.Round(time.Millisecond), time.Since(r.startedAt).Round(time.Millisecond), r.ctx.Err(),
	)
}
