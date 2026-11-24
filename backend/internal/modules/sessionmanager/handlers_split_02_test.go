package sessionmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestCodexDeviceAuthRejectsIncompleteRequest(t *testing.T) {
	e := echo.New()
	h := NewHandlers(&authTestManager{}, "test-secret")
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(codexauth.WorkloadRequest{AttemptID: "cda-only"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, signedCodexAuthRequest(http.MethodPost, "/api/v1/codex-device-auth", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCodexDeviceAuthWithoutLauncherIsNotImplemented(t *testing.T) {
	e := echo.New()
	h := NewHandlers(&proxyTestManager{}, "test-secret")
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(codexauth.WorkloadRequest{
		AttemptID: "cda-0123456789abcdef", CallbackURL: "https://api.example.internal/internal/codex-device-auth",
		Token: "bootstrap-token", ExpiresAt: time.Now().Add(10 * time.Minute),
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, signedCodexAuthRequest(http.MethodPost, "/api/v1/codex-device-auth", body))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

type pooledRestartTestManager struct {
	proxyTestManager
	currentID string
	nextID    string
	paused    bool
}

func (m *pooledRestartTestManager) CurrentSessionSettings(context.Context, string) (*sessionsettings.SessionSettings, error) {
	return nil, nil
}
func (m *pooledRestartTestManager) ValidateSessionRestart(context.Context, string, *sessionsettings.SessionSettings) error {
	return fmt.Errorf("missing manager settings")
}
func (m *pooledRestartTestManager) ValidateSessionRestartWithCurrent(_ context.Context, id string, current, next *sessionsettings.SessionSettings) error {
	if id != "remote-1" {
		return fmt.Errorf("wrong runtime ID")
	}
	m.currentID, m.nextID = current.Session.ID, next.Session.ID
	return sessionsettings.ValidateRestart(current, next)
}
func (m *pooledRestartTestManager) RestartSession(context.Context, string, string, *sessionsettings.SessionSettings) error {
	return nil
}
func (m *pooledRestartTestManager) PauseSession(context.Context, string) error {
	if m.preparedID != "remote-1" {
		return fmt.Errorf("settings not preserved before pause")
	}
	m.paused = true
	return nil
}
func TestPooledRestartRoutesUseParentCurrentSettings(t *testing.T) {
	const secret = "test-secret"
	m := &pooledRestartTestManager{}
	e := echo.New()
	if err := NewHandlers(m, secret).RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	current := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: "public-1", AgentType: "codex-acp"}}
	next := *current
	for _, tc := range []struct {
		action  string
		payload interface{}
	}{
		{"restart/validate", sessionsettings.RestartValidationRequest{SessionSettings: &next, CurrentSettings: current}},
		{"pause", current},
	} {
		body, _ := json.Marshal(tc.payload)
		path := "/api/v1/sessions/remote-1/" + tc.action
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		ts := hmacutil.NowTimestamp()
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(hmacutil.TimestampHeader, ts)
		req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(secret), hmacutil.BuildMessage(req.Method, path, ts, body)))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("%s: status=%d body=%s", tc.action, rec.Code, rec.Body.String())
		}
	}
	if m.currentID != "public-1" || m.nextID != "public-1" || !m.paused {
		t.Fatal("lost current settings or pause")
	}
}
