package cmd

import "testing"

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
