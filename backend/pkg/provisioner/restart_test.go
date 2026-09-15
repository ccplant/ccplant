package provisioner

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPauseAgentWaitsForProcessAndCredentialWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan struct{})
	provisionDone := make(chan struct{})
	close(provisionDone)
	s := New(0, "")
	s.agentCancel = cancel
	s.agentExited = exited
	s.provisionDone = provisionDone
	var completed atomic.Bool
	s.agentWorkers.Add(1)
	go func() {
		<-ctx.Done()
		close(exited)
		time.Sleep(10 * time.Millisecond)
		completed.Store(true)
		s.agentWorkers.Done()
	}()
	if err := s.pauseAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !completed.Load() || s.GetStatus() != Status("paused") {
		t.Fatal("pause returned before runtime stopped")
	}
}
func TestPausedSettingsDoNotStartAgentAfterPodReplacement(t *testing.T) {
	s := New(0, "")
	s.runProvision(context.Background(), &sessionsettings.SessionSettings{Paused: true, Session: sessionsettings.SessionMeta{AgentType: "codex-acp"}})
	if s.GetStatus() != Status("paused") {
		t.Fatalf("status=%s", s.GetStatus())
	}
	if err := s.pauseAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestLifecycleEndpointRequiresManagerCredential(t *testing.T) {
	t.Setenv("PROVISIONER_TOKEN", "manager-secret")
	for _, path := range []string{"/pause", "/restart"} {
		s := New(0, "")
		req := httptest.NewRequest(http.MethodPost, path, nil)
		res := httptest.NewRecorder()
		if path == "/pause" {
			s.handlePauseAgent(res, req)
		} else {
			s.handleRestartAgent(res, req)
		}
		if res.Code != 401 {
			t.Fatalf("unauthenticated %s returned %d", path, res.Code)
		}
	}
}
