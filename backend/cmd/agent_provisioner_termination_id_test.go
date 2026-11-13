package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTerminationSessionIDUsesAllocatedRunnerID(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_ID", "public-session")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "runner-1")
	if got := terminationSessionID(); got != "runner-1" {
		t.Fatalf("terminationSessionID() = %q, want the allocated runner ID", got)
	}
}

func TestTerminationSessionIDFallsBackToSessionID(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_ID", "session-1")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "")
	if got := terminationSessionID(); got != "session-1" {
		t.Fatalf("terminationSessionID() = %q, want the session ID", got)
	}
}

func TestNotifyTerminationSuspendUsesLocalSessionControlEndpoint(t *testing.T) {
	var gotPath, gotAuthorization string
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(manager.Close)

	t.Setenv("PROVISIONER_LOCAL_PROXY_URL", manager.URL)
	t.Setenv("PROVISIONER_PROXY_URL", "http://upstream.invalid")
	t.Setenv("SESSION_CONTROL_TOKEN", "runner-control-token")
	t.Setenv("PROVISIONER_TOKEN", "upstream-token")
	t.Setenv("AGENTAPI_ALLOCATED_SESSION_ID", "runner-1")
	t.Setenv("AGENTAPI_SESSION_ID", "public-session")

	if err := notifyTerminationSuspend(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/internal/session-state/runner-1/terminate" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuthorization != "Bearer runner-control-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
}
