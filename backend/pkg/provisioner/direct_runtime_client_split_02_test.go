package provisioner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	testassert "github.com/stretchr/testify/assert"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestDirectRuntimeInterruptPromptCancelsBeforePrompt(t *testing.T) {
	var methods []string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /status":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"status": "ready"}))
		case http.MethodGet + " /session":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"sessionId": "acp-session"}))
		case http.MethodPost + " /rpc":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			methods = append(methods, body["method"].(string))
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer local.Close()

	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer parent.Close()

	worker := &directRuntimeWorker{
		cfg:    &sessionsettings.ParentRuntimeConfig{Endpoint: parent.URL, SessionID: "public", Token: "secret", Generation: 1},
		client: parent.Client(), agentURL: local.URL, provisionerURL: local.URL,
	}
	worker.executeRequest(context.Background(), core.Command{
		ID: "prompt-1", StreamID: "1-0", Method: http.MethodPost, Path: "/internal/session-interrupt-prompt", Body: []byte(`{"content":"replace current task"}`),
	})

	if !reflect.DeepEqual(methods, []string{"session/cancel", "session/prompt"}) {
		t.Fatalf("RPC methods = %#v", methods)
	}
}

func TestDirectRuntimeRetriesTemporaryUnauthorizedResponse(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var polls atomic.Int32
	ready := make(chan struct{})
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/session-runtime/public-session/requests" {
			http.NotFound(w, r)
			return
		}
		if polls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		select {
		case <-ready:
		default:
			close(ready)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer parent.Close()

	worker := &directRuntimeWorker{
		cfg: &sessionsettings.ParentRuntimeConfig{
			Enabled: true, Endpoint: parent.URL, SessionID: "public-session",
			Token: "runtime-secret", Generation: 4,
		},
		client: parent.Client(), instanceID: "pod-a",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.run(ctx)
		close(done)
	}()

	select {
	case <-ready:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("runtime worker did not reconnect after temporary unauthorized response")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime worker did not stop after cancellation")
	}
	if polls.Load() < 2 {
		t.Fatalf("polls = %d, want at least 2", polls.Load())
	}
}

func TestConfirmRuntimeStartRejectsFencedGeneration(t *testing.T) {
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("generation") != "2" || r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing generation or runtime token")
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer parent.Close()
	err := confirmRuntimeStart(context.Background(), &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: parent.URL, SessionID: "session", Token: "token", Generation: 2})
	if err == nil {
		t.Fatal("fenced runtime was allowed to provision")
	}
}

type promptHoldFixture struct {
	worker     *directRuntimeWorker
	status     atomic.Value
	statusCode atomic.Int32
	rpcCalls   atomic.Int32
	getCalls   atomic.Int32
	frames     chan core.ResponseFrame
}

func newPromptHoldFixture(t *testing.T, status string, statusCode int) *promptHoldFixture {
	t.Helper()
	previous := directRuntimeReadyPollInterval
	directRuntimeReadyPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { directRuntimeReadyPollInterval = previous })

	fixture := &promptHoldFixture{frames: make(chan core.ResponseFrame, 16)}
	fixture.status.Store(status)
	fixture.statusCode.Store(int32(statusCode))
	provisioner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(int(fixture.statusCode.Load()))
		testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"status": fixture.status.Load().(string)}))
	}))
	t.Cleanup(provisioner.Close)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /status":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"agent_type": "claude-acp", "status": "stable"}))
		case http.MethodGet + " /session":
			testassert.NoError(t, json.NewEncoder(w).Encode(map[string]string{"sessionId": "acp-session"}))
		case http.MethodPost + " /rpc":
			fixture.rpcCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet + " /messages":
			fixture.getCalls.Add(1)
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(agent.Close)
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []core.ResponseFrame `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, frame := range body.Frames {
			fixture.frames <- frame
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(parent.Close)
	fixture.worker = &directRuntimeWorker{
		cfg:    &sessionsettings.ParentRuntimeConfig{Endpoint: parent.URL, SessionID: "public", Token: "secret", Generation: 1},
		client: parent.Client(), agentURL: agent.URL, provisionerURL: provisioner.URL,
	}
	return fixture
}

func promptCommand(deadline time.Time) core.Command {
	return core.Command{
		ID: "prompt-1", StreamID: "1-0", Method: http.MethodPost, Path: "/internal/session-prompt",
		Body: []byte(`{"content":"run after resume"}`), Deadline: deadline,
	}
}

func TestDirectRuntimeHoldsPromptUntilAgentIsReady(t *testing.T) {
	fixture := newPromptHoldFixture(t, "provisioning", http.StatusOK)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.worker.execute(context.Background(), promptCommand(time.Now().Add(time.Minute)))
	}()

	time.Sleep(100 * time.Millisecond)
	if got := fixture.rpcCalls.Load(); got != 0 {
		t.Fatalf("prompt reached the agent before ready: rpc calls = %d", got)
	}
	select {
	case frame := <-fixture.frames:
		t.Fatalf("frame was uploaded before ready: %#v", frame)
	default:
	}

	fixture.status.Store("ready")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("prompt was not delivered after the agent became ready")
	}
	if got := fixture.rpcCalls.Load(); got != 1 {
		t.Fatalf("rpc calls = %d, want 1", got)
	}
	select {
	case frame := <-fixture.frames:
		if frame.Status != http.StatusNoContent || !frame.Done || frame.CommandStreamID != "1-0" {
			t.Fatalf("frame = %#v", frame)
		}
	default:
		t.Fatal("completion frame was not uploaded")
	}
}

func TestDirectRuntimeLeavesExpiredPromptUnacknowledged(t *testing.T) {
	fixture := newPromptHoldFixture(t, "provisioning", http.StatusOK)
	fixture.worker.execute(context.Background(), promptCommand(time.Now().Add(150*time.Millisecond)))

	if got := fixture.rpcCalls.Load(); got != 0 {
		t.Fatalf("rpc calls = %d, want 0", got)
	}
	select {
	case frame := <-fixture.frames:
		t.Fatalf("expired prompt was acknowledged: %#v", frame)
	default:
	}
}

func TestDirectRuntimeLeavesPromptUnacknowledgedWhileProvisionFailed(t *testing.T) {
	fixture := newPromptHoldFixture(t, "error", http.StatusInternalServerError)
	fixture.worker.execute(context.Background(), promptCommand(time.Now().Add(150*time.Millisecond)))

	if got := fixture.rpcCalls.Load(); got != 0 {
		t.Fatalf("rpc calls = %d, want 0", got)
	}
	select {
	case frame := <-fixture.frames:
		t.Fatalf("prompt was acknowledged while provisioning failed: %#v", frame)
	default:
	}
}

func TestDirectRuntimeDoesNotHoldOrdinaryRequests(t *testing.T) {
	fixture := newPromptHoldFixture(t, "provisioning", http.StatusOK)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.worker.execute(context.Background(), core.Command{
			ID: "request-1", StreamID: "2-0", Method: http.MethodGet, Path: "/messages", Deadline: time.Now().Add(time.Minute),
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary request was held while provisioning")
	}
	if got := fixture.getCalls.Load(); got != 1 {
		t.Fatalf("forwarded requests = %d, want 1", got)
	}
}
