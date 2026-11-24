package controllers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestResumeRemoteSessionRefreshesCredentialsFromSavedInput(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	autoSuspendEnabled := true
	oldProvisionSettings, err := json.Marshal(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID: "user-1", Scope: string(entities.ScopeUser),
			AutoSuspendEnabled: &autoSuspendEnabled, AutoSuspendMinutes: 5,
		},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{Endpoint: "http://parent.internal:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(entities.StartRequest{Scope: entities.ScopeUser})
	if err != nil {
		t.Fatal(err)
	}
	store := &resumeConfigurationStore{
		allocationReader: &allocationReader{allocation: &sessionrunnercore.Allocation{
			ProvisionSettings: oldProvisionSettings, RuntimeToken: "runtime-token", Generation: 2,
		}},
		configuration: &sessionrunnercore.Configuration{
			SessionID: "public-id", UserID: "user-1", Scope: string(entities.ScopeUser), Input: input,
		},
	}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, resumeSettingsCreator{},
		controllers.WithSessionRouteRepository(&deletionRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
			UserID: "user-1", Scope: string(entities.ScopeUser), Status: "suspended",
		}}),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(store),
	)
	ctx, _ := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/resume", "public-id")
	auth.SetCredentialContext(ctx, &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "fresh-token"})
	if err := controller.ResumeSession(ctx); err != nil {
		t.Fatal(err)
	}
	var got sessionsettings.SessionSettings
	if err := json.Unmarshal(tunnel.body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Env["GITHUB_TOKEN"] != "fresh-token" {
		t.Fatalf("GITHUB_TOKEN = %q, want refreshed credential", got.Env["GITHUB_TOKEN"])
	}
	if got.Session.AutoSuspendEnabled == nil || !*got.Session.AutoSuspendEnabled || got.Session.AutoSuspendMinutes != 5 {
		t.Fatalf("resume auto-suspend policy = enabled %v minutes %d, want preserved true/5", got.Session.AutoSuspendEnabled, got.Session.AutoSuspendMinutes)
	}
	if got.ParentRuntime == nil || got.ParentRuntime.Endpoint != "http://parent.internal:8080" {
		t.Fatalf("resume parent runtime = %#v, want endpoint preserved across settings reload", got.ParentRuntime)
	}
}

func TestRouteToSessionRequiresOutboundManagerConnection(t *testing.T) {
	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		}}),
	)
	ctx, _ := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	err := controller.RouteToSession(ctx)
	if err == nil {
		t.Fatal("expected manager route without outbound connection to be rejected")
	}
	if len(manager.ensuredIDs) != 0 {
		t.Fatalf("external route unexpectedly ensured local IDs %v", manager.ensuredIDs)
	}
}

func TestRouteToSessionUsesDirectSessionRuntime(t *testing.T) {
	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}
	tunnel := &directRuntimeTunnel{}
	routeRepo := &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a", Transport: "direct_session_runtime",
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.managerID != "public-id" || tunnel.path != "/status" {
		t.Fatalf("direct tunnel manager=%q path=%q", tunnel.managerID, tunnel.path)
	}
	if routeRepo.route.Status != "active" || routeRepo.route.StatusUpdatedAt.IsZero() {
		t.Fatalf("persisted route status=%q updated_at=%v, want active with timestamp", routeRepo.route.Status, routeRepo.route.StatusUpdatedAt)
	}
}

func TestRouteToBuiltInSessionUsesOutboundDirectRuntime(t *testing.T) {
	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"public-id": {id: "public-id", userID: "user-1", scope: entities.ScopeUser, status: "active"},
	}}}
	tunnel := &directRuntimeTunnel{}
	routeRepo := &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "public-id", Transport: repositories.SessionRouteTransportDirectRuntime,
		UserID: "user-1", Scope: string(entities.ScopeUser), Status: "active",
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/messages", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.managerID != "public-id" || tunnel.path != "/messages" {
		t.Fatalf("direct tunnel manager=%q path=%q", tunnel.managerID, tunnel.path)
	}
}

func TestRouteToSuspendedRemoteSessionTransparentlyStartsResume(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser), Status: "suspended",
	}}
	var statusWhenResumeSent string
	tunnel.onDo = func() { statusWhenResumeSent = routeRepo.route.Status }
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/messages", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if tunnel.path != "/api/v1/sessions/remote-id/resume" || !strings.Contains(rec.Body.String(), `"code":"session_resuming"`) {
		t.Fatalf("resume path=%q body=%s", tunnel.path, rec.Body.String())
	}
	if routeRepo.route.Status != "resuming" {
		t.Fatalf("route status=%q, want resuming", routeRepo.route.Status)
	}
	if statusWhenResumeSent != "resuming" {
		t.Fatalf("route status when resume was sent=%q, want resuming", statusWhenResumeSent)
	}
}

func TestRouteToSuspendedRemoteSessionStatusDoesNotResume(t *testing.T) {
	tunnel := &lifecycleTunnel{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(&deletionRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
			UserID: "user-1", Scope: string(entities.ScopeUser), Status: "suspended",
		}}),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")
	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"suspended"`) {
		t.Fatalf("status=%d body=%s, want suspended status", rec.Code, rec.Body.String())
	}
	if tunnel.path != "" {
		t.Fatalf("status polling unexpectedly called manager path %q", tunnel.path)
	}
}

func TestRouteToResumingRemoteSessionReturnsStructuredRecoveryResponse(t *testing.T) {
	tunnel := &lifecycleTunnel{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(&deletionRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
			UserID: "user-1", Scope: string(entities.ScopeUser), Status: "resuming",
		}}),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/messages", "public-id")
	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" || !strings.Contains(rec.Body.String(), `"code":"session_resuming"`) {
		t.Fatalf("status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if tunnel.path != "" {
		t.Fatalf("resuming request unexpectedly called manager path %q", tunnel.path)
	}
}

func TestRemoteStatusChangeReachesStatusWait(t *testing.T) {
	manager := &statusWatchingSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}},
		events:             make(chan repositories.SessionStatusEvent),
	}
	routeRepo := &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime, UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(&directRuntimeTunnel{}),
	)

	waitCtx, waitRec := routeContext(echo.New(), http.MethodGet, "/sessions/status/wait?timeout=2", "")
	done := make(chan error, 1)
	go func() { done <- controller.WaitSessionsStatus(waitCtx) }()
	time.Sleep(20 * time.Millisecond)

	statusCtx, _ := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")
	if err := controller.RouteToSession(statusCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote status event was not delivered")
	}
	var evt repositories.SessionStatusEvent
	if err := json.Unmarshal(waitRec.Body.Bytes(), &evt); err != nil {
		t.Fatal(err)
	}
	if evt.SessionID != "public-id" || evt.Status != "active" {
		t.Fatalf("event = %+v, want public-id active", evt)
	}
}
