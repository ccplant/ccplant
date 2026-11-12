package provisioner

import (
	"context"
	"errors"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"net/http"
	"net/http/httptest"
	"os"
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
func TestProvisionFailureBeforeRestoreRecordsAbortedMarker(t *testing.T) {
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer parent.Close()
	for _, restartInPlace := range []bool{false, true} {
		t.Setenv("TMPDIR", t.TempDir())
		s := New(0, "")
		s.runProvision(context.Background(), &sessionsettings.SessionSettings{
			RestartInPlace: restartInPlace,
			Session:        sessionsettings.SessionMeta{ID: "session-1", AgentType: "codex-acp"},
			ParentRuntime:  &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: parent.URL, SessionID: "session-1", Generation: 1},
		})
		if s.GetStatus() != StatusError {
			t.Fatalf("restart_in_place=%v status=%s", restartInPlace, s.GetStatus())
		}
		_, err := os.Stat(sessionProvisionAbortedMarkerPath("session-1"))
		if restartInPlace != os.IsNotExist(err) {
			t.Fatalf("restart_in_place=%v aborted marker stat err=%v", restartInPlace, err)
		}
		want := errSessionSetupFailed
		if restartInPlace {
			want = errSessionSetupInProgress
		}
		if err := sessionSetupCheckpointError("session-1"); !errors.Is(err, want) {
			t.Fatalf("restart_in_place=%v checkpoint err=%v, want %v", restartInPlace, err, want)
		}
	}
}
func TestProvisionClearsStaleAbortedMarker(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.WriteFile(sessionProvisionAbortedMarkerPath("session-1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(0, "")
	s.runProvision(context.Background(), &sessionsettings.SessionSettings{
		Paused:  true,
		Session: sessionsettings.SessionMeta{ID: "session-1", AgentType: "codex-acp"},
	})
	if _, err := os.Stat(sessionProvisionAbortedMarkerPath("session-1")); !os.IsNotExist(err) {
		t.Fatalf("stale aborted marker survived: %v", err)
	}
}
func TestScheduleSessionSuspendFromReadyUsesSessionControlToken(t *testing.T) {
	var gotPath, gotAuth string
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	t.Setenv("PROVISIONER_LOCAL_PROXY_URL", manager.URL)
	t.Setenv("PROVISIONER_PROXY_URL", "http://parent.invalid")
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")
	t.Setenv("SESSION_CONTROL_TOKEN", "control-token")
	t.Setenv("AGENTAPI_SESSION_ID", "runner-1")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "")
	if err := scheduleSessionSuspendFromReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/internal/session-state/runner-1/suspend" || gotAuth != "Bearer control-token" {
		t.Fatalf("path=%q auth=%q", gotPath, gotAuth)
	}
}
