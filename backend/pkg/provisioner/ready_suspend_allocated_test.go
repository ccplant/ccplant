package provisioner

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingTransport struct {
	path string
	auth string
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	t.auth = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func useRecordingTransport(t *testing.T) *recordingTransport {
	t.Helper()
	recorder := &recordingTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = previous })
	return recorder
}

func TestScheduleSessionSuspendFromReadyUsesAllocatedSessionID(t *testing.T) {
	recorder := useRecordingTransport(t)
	t.Setenv("PROVISIONER_LOCAL_PROXY_URL", "http://manager.invalid")
	t.Setenv("PROVISIONER_PROXY_URL", "http://parent.invalid")
	t.Setenv("SESSION_CONTROL_TOKEN", "runner-control-token")
	t.Setenv("AGENTAPI_SESSION_ID", "public-session")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "runner-1")

	if err := scheduleSessionSuspendFromReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recorder.path != "/internal/session-state/runner-1/suspend" {
		t.Fatalf("path = %q, want the allocated runner ID", recorder.path)
	}
	if recorder.auth != "Bearer runner-control-token" {
		t.Fatalf("authorization header was not the session control token")
	}
}

func TestScheduleSessionSuspendFromReadyFallsBackToSessionID(t *testing.T) {
	recorder := useRecordingTransport(t)
	t.Setenv("PROVISIONER_LOCAL_PROXY_URL", "http://manager.invalid")
	t.Setenv("SESSION_CONTROL_TOKEN", "session-control-token")
	t.Setenv("AGENTAPI_SESSION_ID", "session-1")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "")

	if err := scheduleSessionSuspendFromReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recorder.path != "/internal/session-state/session-1/suspend" {
		t.Fatalf("path = %q, want the session ID", recorder.path)
	}
}
