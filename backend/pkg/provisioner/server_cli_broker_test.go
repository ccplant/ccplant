package provisioner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/agentpolicy"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

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

func TestCLIBrokerPolicyControlsResourceActions(t *testing.T) {
	server := NewWithSocket("", "")
	server.ConfigureCLIBroker("https://example.invalid", "session-1")
	server.brokerKey = "personal-key"
	server.brokerPolicy = &agentpolicy.Policy{Version: agentpolicy.Version, Resources: map[string]agentpolicy.Actions{
		"schedules": {Read: true},
		"sessions":  {Update: true},
	}}

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
