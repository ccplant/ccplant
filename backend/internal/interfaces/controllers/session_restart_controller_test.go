package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type restartStoreStub struct {
	mu  sync.Mutex
	cfg core.Configuration
}

func (s *restartStoreStub) GetAllocation(context.Context, string) (*core.Allocation, error) {
	return nil, core.ErrNotFound
}
func (s *restartStoreStub) CreateConfiguration(_ context.Context, c *core.Configuration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = *c
	return nil
}
func (s *restartStoreStub) GetConfiguration(context.Context, string) (*core.Configuration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	return &c, nil
}
func (s *restartStoreStub) SaveConfiguration(_ context.Context, c *core.Configuration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.Version != s.cfg.Version {
		return core.ErrConflict
	}
	c.Version++
	c.UpdatedAt = time.Now()
	s.cfg = *c
	return nil
}
func (s *restartStoreStub) UpdateProvisionSettings(context.Context, string, []byte) error { return nil }

type restartManagerStub struct {
	repositories.SessionManager
	old      *sessionsettings.SessionSettings
	paused   bool
	received chan *sessionsettings.SessionSettings
}

func (m *restartManagerStub) GetSession(id string) entities.Session {
	return entities.NewProxySessionWithStatus(id, "alice", entities.ScopeUser, "", nil, time.Now(), "stable")
}
func (m *restartManagerStub) CurrentSessionSettings(context.Context, string) (*sessionsettings.SessionSettings, error) {
	return m.old, nil
}
func (m *restartManagerStub) ValidateSessionRestart(_ context.Context, _ string, s *sessionsettings.SessionSettings) error {
	return sessionsettings.ValidateRestart(m.old, s)
}
func (m *restartManagerStub) PauseSession(context.Context, string) error { m.paused = true; return nil }
func (m *restartManagerStub) RestartSession(_ context.Context, _ string, _ string, s *sessionsettings.SessionSettings) error {
	m.received <- s
	return nil
}

type restartProviderStub struct{ m *restartManagerStub }

func (p restartProviderStub) GetSessionManager() repositories.SessionManager { return p.m }

type restartCreatorStub struct {
	SessionCreator
	m *restartManagerStub
}

func (s restartCreatorStub) ResolveRestartSettings(_ context.Context, id string, start entities.StartRequest, _ string, _ []string) (*sessionsettings.SessionSettings, error) {
	token := "before-stop"
	if s.m.paused {
		token = "latest-after-stop"
	}
	return &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: id, AgentType: "codex-acp"}, Env: map[string]string{"TOKEN": token, "EXPLICIT": start.Environment["EXPLICIT"]}}, nil
}
func restartContext(t *testing.T, user, body, key string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	r := httptest.NewRequest(http.MethodPost, "/sessions/one/restart", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	ctx := e.NewContext(r, w)
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("one")
	ctx.Set("authz_context", &auth.AuthorizationContext{User: entities.NewUser(entities.UserID(user), entities.UserTypeAPIKey, user), PersonalScope: auth.PersonalScopeAuth{UserID: user, CanRead: true, CanCreate: true}})
	return ctx, w
}
func TestRestartReloadsWholeSettingsAfterStopping(t *testing.T) {
	old := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: "one", AgentType: "codex-acp"}, Env: map[string]string{"REMOVED": "old"}}
	m := &restartManagerStub{old: old, received: make(chan *sessionsettings.SessionSettings, 1)}
	raw, _ := json.Marshal(entities.StartRequest{Environment: map[string]string{"EXPLICIT": "keep"}})
	store := &restartStoreStub{cfg: core.Configuration{SessionID: "one", UserID: "alice", Scope: "user", Input: raw}}
	c := NewSessionController(restartProviderStub{m}, restartCreatorStub{m: m}, WithSessionRunnerStore(store))
	ctx, res := restartContext(t, "alice", `{"reload_settings":true}`, "request-1")
	if err := c.RestartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if res.Code != 202 {
		t.Fatalf("status=%d", res.Code)
	}
	select {
	case settings := <-m.received:
		if settings.Env["TOKEN"] != "latest-after-stop" || settings.Env["EXPLICIT"] != "keep" || settings.Env["REMOVED"] != "" || !settings.Restart {
			t.Fatalf("incorrect settings: %#v", settings)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not run")
	}
	deadline := time.Now().Add(time.Second)
	for {
		cfg, _ := store.GetConfiguration(context.Background(), "one")
		if cfg.Phase == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation not completed")
		}
		time.Sleep(time.Millisecond)
	}
	if old.Env["REMOVED"] != "old" || old.Restart {
		t.Fatal("old settings mutated")
	}
	duplicate, _ := restartContext(t, "alice", `{"reload_settings":false}`, "request-1")
	if err := c.RestartSession(duplicate); err == nil || err.(*echo.HTTPError).Code != 409 {
		t.Fatalf("different body reused request ID: %v", err)
	}
}
func TestRestartHoldAndOwnerAuthorization(t *testing.T) {
	store := &restartStoreStub{cfg: core.Configuration{SessionID: "one", UserID: "alice", Scope: "user", Phase: "paused"}}
	c := NewSessionController(nil, nil, WithSessionRunnerStore(store))
	ctx, _ := restartContext(t, "alice", `{}`, "id")
	if err := c.checkRestartHold(ctx); err == nil || err.(*echo.HTTPError).Code != 423 {
		t.Fatalf("hold not enforced: %v", err)
	}
	other, _ := restartContext(t, "bob", `{}`, "id")
	if _, _, err := c.restartConfiguration(other); err == nil || err.(*echo.HTTPError).Code != 403 {
		t.Fatalf("owner check failed: %v", err)
	}
}

func TestRestartHoldAllowsAuthenticatedWorkerDeletion(t *testing.T) {
	store := &restartStoreStub{cfg: core.Configuration{SessionID: "one", UserID: "alice", Scope: "user", Phase: "paused"}}
	c := NewSessionController(nil, nil, WithSessionRunnerStore(store))
	ctx, _ := restartContext(t, "bob", `{}`, "id")
	ctx.Set(workerAuthorizedDeleteContextKey, true)
	if err := c.checkRestartHold(ctx); err != nil {
		t.Fatalf("worker deletion must bypass restart hold and owner check: %v", err)
	}
}

func TestRestartRejectsAnotherUsersCredentials(t *testing.T) {
	m := &restartManagerStub{}
	c := NewSessionController(restartProviderStub{m}, restartCreatorStub{m: m})
	ctx, _ := restartContext(t, "alice", "", "")
	az := auth.GetAuthorizationContext(ctx)
	for _, source := range []string{"session_user", "triggered_user", "github_sender"} {
		raw, _ := json.Marshal(entities.StartRequest{Params: &entities.SessionParams{CredentialSource: source}})
		cfg := &core.Configuration{SessionID: "one", UserID: "bob", Scope: "team", TeamID: "org/team", TriggeredUserID: "bob", Input: raw}
		_, err := c.reloadSessionSettings(context.Background(), cfg, "", az)
		httpErr, ok := err.(*echo.HTTPError)
		if !ok || httpErr.Code != 403 {
			t.Fatalf("source %s: expected forbidden, got %v", source, err)
		}
	}
}

func TestRestartReplacesExplicitStartupInput(t *testing.T) {
	old := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{ID: "one", AgentType: "codex-acp"}}
	m := &restartManagerStub{old: old, received: make(chan *sessionsettings.SessionSettings, 1)}
	store := &restartStoreStub{cfg: core.Configuration{SessionID: "one", UserID: "alice", Scope: "user", Input: []byte(`{"environment":{"EXPLICIT":"old"}}`)}}
	c := NewSessionController(restartProviderStub{m}, restartCreatorStub{m: m}, WithSessionRunnerStore(store))
	ctx, _ := restartContext(t, "alice", `{"startup_input":{"environment":{"EXPLICIT":"replacement"},"triggered_user_id":"attacker"}}`, "replace")
	if err := c.RestartSession(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-m.received:
		if got.Env["EXPLICIT"] != "replacement" {
			t.Fatal("startup input was not replaced")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not finish")
	}
}

func TestRestartManagerErrorPreservesSafeFailureReason(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, want string
	}{
		{404, `{"message":"Not Found"}`, "manager restart endpoint or session not found"},
		{501, `{"message":"restart unavailable"}`, "manager does not support conversation restart"},
		{422, `{"message":"conversation checkpoint storage is required for restart"}`, "conversation checkpoint storage is required for restart"},
		{422, `{"message":"secret-token-value"}`, "manager restart request failed (HTTP 422)"},
		{503, `{"message":"secret-token-value"}`, "manager restart request failed (HTTP 503)"},
	} {
		response := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}
		err := restartManagerResponseError(response).(*echo.HTTPError)
		if err.Code != tc.status || !strings.Contains(fmt.Sprint(err.Message), tc.want) || strings.Contains(fmt.Sprint(err.Message), "secret-token-value") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
