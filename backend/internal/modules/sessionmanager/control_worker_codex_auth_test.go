package sessionmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

// TestControlWorkerExecutesCodexDeviceAuthCommand exercises the exact path a
// parent codex device auth request takes: the outbound control worker turns a
// tunnel command into an HMAC-signed local request against the forwarding
// surface, which must reach the session manager's workload launcher.
func TestControlWorkerExecutesCodexDeviceAuthCommand(t *testing.T) {
	manager := &authTestManager{}
	const secret = "control-secret"
	e := echo.New()
	h := NewHandlers(manager, secret)
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(e)
	defer local.Close()

	var frames []core.ResponseFrame
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Frames []core.ResponseFrame `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode frames: %v", err)
		}
		frames = append(frames, payload.Frames...)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	worker := NewControlWorker(upstream.URL, "connection-token", "", local.URL, "test-instance", secret)
	body, err := json.Marshal(codexauth.WorkloadRequest{
		AttemptID:   "cda-0123456789abcdef",
		CallbackURL: "https://api.example.internal/internal/codex-device-auth",
		Token:       "bootstrap-token",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	command := core.Command{
		ID: "cmd-1", ManagerID: "manager-1", SessionID: "cda-0123456789abcdef",
		Method: http.MethodPost, Path: "/api/v1/codex-device-auth", Body: body,
		Headers:  map[string][]string{"Content-Type": {"application/json"}},
		Deadline: time.Now().Add(30 * time.Second), CreatedAt: time.Now().UTC(),
	}
	worker.executeCommand(context.Background(), command)

	if len(manager.started) != 1 {
		t.Fatalf("started workloads = %d, want 1; frames = %#v", len(manager.started), frames)
	}
	if manager.started[0].AttemptID != "cda-0123456789abcdef" {
		t.Fatalf("attempt = %#v", manager.started[0])
	}
	if len(frames) == 0 || frames[0].Status != http.StatusAccepted || !frames[len(frames)-1].Done {
		t.Fatalf("frames = %#v", frames)
	}
}

// TestControlWorkerCancelsCodexDeviceAuthCommand covers the DELETE leg.
func TestControlWorkerCancelsCodexDeviceAuthCommand(t *testing.T) {
	manager := &authTestManager{}
	const secret = "control-secret"
	e := echo.New()
	h := NewHandlers(manager, secret)
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(e)
	defer local.Close()

	var frames []core.ResponseFrame
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Frames []core.ResponseFrame `json:"frames"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		frames = append(frames, payload.Frames...)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	worker := NewControlWorker(upstream.URL, "connection-token", "", local.URL, "test-instance", secret)
	command := core.Command{
		ID: "cmd-2", ManagerID: "manager-1", SessionID: "cda-0123456789abcdef",
		Method: http.MethodDelete, Path: "/api/v1/codex-device-auth/cda-0123456789abcdef",
		Deadline: time.Now().Add(30 * time.Second), CreatedAt: time.Now().UTC(),
	}
	worker.executeCommand(context.Background(), command)

	if len(manager.cancelled) != 1 || manager.cancelled[0] != "cda-0123456789abcdef" {
		t.Fatalf("cancelled = %#v", manager.cancelled)
	}
	if len(frames) == 0 || frames[0].Status != http.StatusNoContent || !frames[len(frames)-1].Done {
		t.Fatalf("frames = %#v", frames)
	}
}
