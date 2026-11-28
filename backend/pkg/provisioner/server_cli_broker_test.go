package provisioner

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/agentpolicy"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestCLIBrokerUnixSocketAcceptsUnauthenticatedCallerAndAuthenticatesUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer session-key" {
			t.Errorf("upstream Authorization = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	socketPath := filepath.Join(t.TempDir(), "provisioner.sock")
	server := NewWithSocket(socketPath, "")
	server.ConfigureCLIBroker(upstream.URL, "session-1")
	server.brokerKey = "session-key"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("server shutdown: %v", err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provisioner socket was not created")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	req, err := http.NewRequest(http.MethodPost, "http://unix/notifications/send", strings.NewReader(`{"title":"done","body":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("local request unexpectedly has authorization")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestCLIBrokerAuthenticatesAndScopesNotification(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer personal-key" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["session_id"] != "session-1" {
			t.Errorf("session_id = %#v", body["session_id"])
		}
		if _, ok := body["user_id"]; ok {
			t.Error("user_id must not be forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer upstream.Close()

	server := NewWithSocket("", "")
	server.ConfigureCLIBroker(upstream.URL, "session-1")
	server.brokerKey = "personal-key"
	req := httptest.NewRequest(http.MethodPost, "/notifications/send", strings.NewReader(`{"title":"done","body":"ok","user_id":"other"}`))
	rec := httptest.NewRecorder()
	server.handleCLIBroker(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubCredentialsReturnsProvisionedTokenWithoutUpstreamRequest(t *testing.T) {
	server := NewWithSocket("", "")
	server.githubToken = "direct-token"
	req := httptest.NewRequest(http.MethodGet, "/github-credentials?repository=acme/widgets", nil)
	rec := httptest.NewRecorder()

	server.handleGitHubCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got, want := rec.Body.String(), "{\"token\":\"direct-token\",\"username\":\"x-access-token\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestGitHubCredentialsProxiesBrokerThroughProvisioner(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer lease-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("repository"); got != "acme/widgets" {
			t.Errorf("repository = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"x-access-token","token":"upstream-token"}`))
	}))
	defer upstream.Close()

	server := NewWithSocket("", "")
	server.githubBrokerURL = upstream.URL
	server.githubBrokerToken = "lease-token"
	req := httptest.NewRequest(http.MethodGet, "/github-credentials?repository=acme%2Fwidgets", nil)
	rec := httptest.NewRecorder()

	server.handleGitHubCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "upstream-token") {
		t.Fatalf("body = %q", got)
	}
}

func TestCLIBrokerRejectsAnotherSession(t *testing.T) {
	server := NewWithSocket("", "")
	server.ConfigureCLIBroker("https://example.invalid", "session-1")
	server.brokerKey = "personal-key"
	req := httptest.NewRequest(http.MethodDelete, "/sessions/session-2", nil)
	rec := httptest.NewRecorder()
	server.handleCLIBroker(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestConfigureCLIBrokerForClaimedStockSessionUsesPublicSessionID(t *testing.T) {
	server := NewWithSocket("", "")
	server.ConfigureCLIBroker("https://manager.example", "allocation-id")
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{ID: "public-session-id"},
		Env: map[string]string{
			"AGENTAPI_KEY":            "personal-key",
			"AGENTAPI_PROXY_ENDPOINT": "https://parent.example/api/v1",
		},
	}

	server.configureCLIBrokerForSession(settings)

	if server.brokerSessionID != "public-session-id" {
		t.Fatalf("broker session ID = %q, want public-session-id", server.brokerSessionID)
	}
	if server.brokerURL != "https://parent.example/api/v1" {
		t.Fatalf("broker URL = %q", server.brokerURL)
	}
	if server.brokerKey != "personal-key" {
		t.Fatal("broker key was not captured")
	}
}

func TestConfigureCLIBrokerForSessionCapturesGitHubCredentials(t *testing.T) {
	server := NewWithSocket("", "")
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{ID: "session-1"},
		Env: map[string]string{
			"GITHUB_TOKEN":                 "direct-token",
			"AGENTAPI_GITHUB_BROKER_URL":   "https://broker.example.test/credentials",
			"AGENTAPI_GITHUB_BROKER_TOKEN": "lease-token",
		},
	}

	if !server.configureCLIBrokerForSession(settings) {
		t.Fatal("GitHub credentials were not detected")
	}
	if server.githubToken != "direct-token" || server.githubBrokerURL == "" || server.githubBrokerToken != "lease-token" {
		t.Fatal("GitHub credentials were not captured")
	}
}

func TestCLIBrokerPolicyControlsResourceActions(t *testing.T) {
	server := NewWithSocket("", "")
	server.ConfigureCLIBroker("https://example.invalid", "session-1")
	server.brokerKey = "personal-key"
	var err error
	server.brokerPolicy, err = agentpolicy.Parse("version: v1\nresources:\n  schedules:\n    read: true\n  sessions:\n    update: true\n")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/schedules", true},
		{http.MethodPost, "/schedules", false},
		{http.MethodPost, "/session-1/message", true},
		{http.MethodPost, "/session-2/message", false},
		{http.MethodPost, "/internal/usage-events", true},
	} {
		if got := allowedCLIBrokerRequest(httptest.NewRequest(tt.method, tt.path, nil), "session-1", server.brokerPolicy); got != tt.allowed {
			t.Errorf("%s %s allowed = %v, want %v", tt.method, tt.path, got, tt.allowed)
		}
	}
}
