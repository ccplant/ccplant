package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func executeAdmin(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := newAdminCommand()
	var output strings.Builder
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(append([]string{"--endpoint", server.URL, "--api-key", "secret"}, args...))
	err := cmd.Execute()
	return output.String(), err
}

func TestAdminRunners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/session-runners" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("scope"); got != "system" {
			t.Fatalf("scope = %q", got)
		}
		_, _ = w.Write([]byte(`{"session_runners":[{"id":"runner-1","manager_id":"manager-1","manager_name":"native","pool":"default","status":"running","session_id":"session-1","online":true}]}`))
	}))
	defer server.Close()

	output, err := executeAdmin(t, server, "runners")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"RUNNER ID", "runner-1", "running", "session-1", "native", "default"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestAdminSessionJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sessions":[{"session_id":"session-1","user_id":"user-1","status":"running","started_at":"2026-09-30T00:00:00Z","port":8080}]}`))
	}))
	defer server.Close()

	output, err := executeAdmin(t, server, "--json", "session", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"session_id": "session-1"`) || !strings.Contains(output, `"status": "running"`) {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestAdminSessionLogsResolveRunner(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/admin/session-runners":
			_, _ = w.Write([]byte(`{"session_runners":[{"id":"runner-1","manager_id":"manager-1","session_id":"session-1"}]}`))
		case "/admin/session-runners/runner-1/logs":
			if r.URL.Query().Get("manager_id") != "manager-1" || r.URL.Query().Get("session_id") != "session-1" || r.URL.Query().Get("scope") != "system" || r.URL.Query().Get("tail") != "42" {
				t.Fatalf("query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"lines":["first","second"],"source":"pod/runner-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output, err := executeAdmin(t, server, "logs", "session", "session-1", "--tail", "42")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || output != "first\nsecond\n" {
		t.Fatalf("requests = %d, output = %q", requests, output)
	}
}

func TestAdminManagerLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/session-managers/manager-1/logs" || r.URL.Query().Get("tail") != "200" {
			t.Fatalf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"lines":["manager log"],"source":"daemon"}`))
	}))
	defer server.Close()

	output, err := executeAdmin(t, server, "logs", "session-manager", "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if output != "manager log\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestAdminLogsRejectInvalidTail(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	_, err := executeAdmin(t, server, "logs", "runner", "runner-1", "--tail", "0")
	if err == nil || !strings.Contains(err.Error(), "between 1 and 5000") {
		t.Fatalf("err = %v", err)
	}
}
