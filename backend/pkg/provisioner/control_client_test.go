package provisioner

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFetchProvisionSettingsUsesControlTokenAndMarksInContainerReload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/session-control/session-1/provision-settings" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer control-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session":{"id":"session-1","user_id":"user-1"},"files":[{"path":"/home/agentapi/.codex/auth.json","content":"fresh"}]}`))
	}))
	defer server.Close()

	settings, err := fetchProvisionSettings(context.Background(), server.Client(), PullClientConfig{
		ProxyURL: server.URL, SessionControlToken: "control-secret", SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Session.InContainerReload {
		t.Fatal("fetched settings were not marked as an in-container reload")
	}
	if len(settings.Files) != 1 || settings.Files[0].Content != "fresh" {
		t.Fatalf("unexpected settings files: %#v", settings.Files)
	}
}
