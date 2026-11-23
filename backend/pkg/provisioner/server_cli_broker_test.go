package provisioner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
