package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPollControlCommandsUsesHTTPSAPIAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("after"); got != "12-3" {
			t.Fatalf("after = %q, want 12-3", got)
		}
		if got := r.URL.Query().Get("wait"); got != "30s" {
			t.Fatalf("wait = %q, want 30s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer provisioner-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commands":[{"id":"cmd-1","stream_id":"12-4","type":"cancel"}],"next_cursor":"12-4"}`))
	}))
	defer server.Close()

	commands, err := pollControlCommands(context.Background(), server.Client(), PullClientConfig{
		ProxyURL: server.URL, Token: "provisioner-secret", SessionControlToken: "provisioner-secret", SessionID: "session-1",
	}, "12-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].ID != "cmd-1" || commands[0].StreamID != "12-4" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
}

func TestPollControlCommandsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	commands, err := pollControlCommands(context.Background(), server.Client(), PullClientConfig{
		ProxyURL: server.URL, Token: "token", SessionControlToken: "token", SessionID: "session-1",
	}, "0-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected no commands, got %#v", commands)
	}
}

func TestExecuteControlCommandCheckpointsThroughProxyBinary(t *testing.T) {
	t.Setenv("AGENTAPI_SESSION_ID", "")
	t.Setenv("CCPLANT_BINARY_PATH", "/bin/true")
	err := executeControlCommand(context.Background(), http.DefaultClient, "codex-acp", controlCommand{Type: "checkpoint_session_state"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecuteControlCommandParksHerdrAfterCheckpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	marker := filepath.Join(dir, "parked")
	hc := filepath.Join(dir, "hc")
	if err := os.WriteFile(hc, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HC_TEST_MARKER\"\nif [ \"$*\" = 'ls' ]; then\n  printf '%s\\n' '{\"terminals\":[{\"address\":\"t1\"},{\"address\":\"t2\"}]}'\nfi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_TEST_MARKER", marker)
	t.Setenv("AGENTAPI_SESSION_ID", "")
	t.Setenv("CCPLANT_BINARY_PATH", "/bin/true")
	payload := json.RawMessage(`{"park_herdr":true}`)
	if err := executeControlCommand(context.Background(), http.DefaultClient, "codex-acp", controlCommand{Type: "checkpoint_session_state", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ls\nkeep t1\nkeep t2\nserver stop\n" {
		t.Fatalf("hc calls = %q", got)
	}
	if _, err := os.Stat(herdrParkMarkerPath()); err != nil {
		t.Fatalf("park marker: %v", err)
	}
}

func TestExecuteControlCommandRejectsCheckpointUntilSetupCompletes(t *testing.T) {
	tests := []struct {
		name     string
		aborted  bool
		restored bool
		payload  string
		want     error
		reason   string
	}{
		{name: "setup in progress", want: errSessionSetupInProgress, reason: "setup_in_progress"},
		{name: "setup failed", aborted: true, want: errSessionSetupFailed, reason: "setup_failed"},
		{name: "template snapshot during setup", payload: `{"snapshot_id":"tpl_1"}`, want: errSessionSetupInProgress, reason: "setup_in_progress"},
		{name: "restore completed", aborted: true, restored: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			t.Setenv("AGENTAPI_SESSION_ID", "session-1")
			t.Setenv("CCPLANT_BINARY_PATH", "/bin/false")
			if tt.aborted {
				if err := os.WriteFile(sessionProvisionAbortedMarkerPath("session-1"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.restored {
				if err := os.WriteFile(sessionRestoreMarkerPath("session-1"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := controlCommand{Type: "checkpoint_session_state"}
			if tt.payload != "" {
				command.Payload = json.RawMessage(tt.payload)
			}
			err := executeControlCommand(context.Background(), http.DefaultClient, "codex-acp", command)
			if tt.want == nil {
				if err == nil || sessionSetupCheckpointReason(err) != "" {
					t.Fatalf("err=%v, want the backup command failure", err)
				}
				return
			}
			if !errors.Is(err, tt.want) || sessionSetupCheckpointReason(err) != tt.reason {
				t.Fatalf("err=%v reason=%q, want %v %q", err, sessionSetupCheckpointReason(err), tt.want, tt.reason)
			}
		})
	}
}

func TestSessionSetupCheckpointStatus(t *testing.T) {
	if got := sessionSetupCheckpointStatus(errSessionSetupInProgress); got != http.StatusTooEarly {
		t.Fatalf("in progress status=%d", got)
	}
	if got := sessionSetupCheckpointStatus(errSessionSetupFailed); got != http.StatusFailedDependency {
		t.Fatalf("failed status=%d", got)
	}
	if got := sessionSetupCheckpointStatus(errors.New("backup failed")); got != 0 {
		t.Fatalf("other error status=%d", got)
	}
}
