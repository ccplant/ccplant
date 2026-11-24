package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestResumeSessionLocalAliasRestoringReturnsPublicSessionID(t *testing.T) {
	manager := &ensuringSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
			"remote-id": {id: "remote-id", userID: "user-1", scope: entities.ScopeUser},
		}},
		restoring: true,
	}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id",
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/resume", "public-id")

	if err := controller.ResumeSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("response status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["session_id"] != "public-id" {
		t.Fatalf("response session_id = %v, want public-id", response["session_id"])
	}
	if response["status"] != "resuming" {
		t.Fatalf("response status = %v, want resuming", response["status"])
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "remote-id" {
		t.Fatalf("ensured IDs = %v, want [remote-id]", manager.ensuredIDs)
	}
}

func TestDeleteSessionAlreadyAbsentIsIdempotent(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	store := &allocationReader{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRunnerStore(store),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/missing-id", "missing-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["session_id"] != "missing-id" || response["status"] != "terminated" {
		t.Fatalf("response = %#v, want missing-id terminated", response)
	}
	if store.deletedConfiguration != "missing-id" {
		t.Fatalf("deleted configuration = %q, want missing-id", store.deletedConfiguration)
	}
}

func TestDeleteManagedSessionRecordsTerminatedStatus(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"session-1": {id: "session-1", userID: "user-1", scope: entities.ScopeUser, status: "active"},
	}}
	creator := &fakeSessionCreator{}
	lastStatusAt := time.Now().Add(time.Hour)
	usageRecorder := &statusUsageRecorder{dimensions: repositories.SessionUsageDimensions{
		Pool: "linux", Scope: string(entities.ScopeUser), PrincipalID: "user-1", LastStatusAt: lastStatusAt,
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, creator,
		controllers.WithSessionStatusUsageRecorder(usageRecorder),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/session-1", "session-1")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(creator.deleted, []string{"session-1"}) {
		t.Fatalf("deleted sessions = %v, want [session-1]", creator.deleted)
	}
	if len(usageRecorder.events) != 1 || usageRecorder.events[0].Status != "terminated" {
		t.Fatalf("usage events = %#v, want one terminated event", usageRecorder.events)
	}
	if want := lastStatusAt.Add(time.Nanosecond); !usageRecorder.events[0].Timestamp.Equal(want) {
		t.Fatalf("terminal timestamp = %s, want %s", usageRecorder.events[0].Timestamp, want)
	}
}

func TestDeleteLocalSessionAliasRemovesPoolAllocation(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"runtime-id": {id: "runtime-id", userID: "user-1", scope: entities.ScopeUser, status: "active"},
	}}
	creator := &fakeSessionCreator{}
	routes := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runtime-id", UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, creator,
		controllers.WithSessionRouteRepository(routes),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(creator.deleted, []string{"runtime-id"}) {
		t.Fatalf("deleted sessions = %v, want [runtime-id]", creator.deleted)
	}
	if !reflect.DeepEqual(creator.deletedAllocations, []string{"public-id"}) {
		t.Fatalf("deleted allocations = %v, want [public-id]", creator.deletedAllocations)
	}
	if !routes.deleted {
		t.Fatal("session route was not deleted")
	}
}

func TestDeleteDirectRuntimeUsesAllocatedRunnerID(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "allocated-runner-id", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime,
		UserID:    "user-1", Scope: string(entities.ScopeUser),
	}}
	store := &allocationReader{}
	usageRecorder := &statusUsageRecorder{dimensions: repositories.SessionUsageDimensions{
		Pool: "linux", Scope: string(entities.ScopeUser), PrincipalID: "user-1",
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(store),
		controllers.WithSessionStatusUsageRecorder(usageRecorder),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("response status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.path != "/api/v1/sessions/allocated-runner-id" {
		t.Fatalf("delete path = %q, want allocated runner workload ID", tunnel.path)
	}
	if !tunnel.enqueued {
		t.Fatal("direct-runtime deletion was not durably enqueued")
	}
	if !routeRepo.saved || routeRepo.deleted || routeRepo.route.Status != "terminating" || routeRepo.route.DeletionRequestID == "" {
		t.Fatalf("route must remain terminating until manager completion: %#v", routeRepo)
	}

	tunnel.done = true
	tunnel.status = http.StatusNoContent
	ctx, rec = routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")
	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !routeRepo.deleted {
		t.Fatalf("completed deletion was not finalized: status=%d repo=%#v", rec.Code, routeRepo)
	}
	if store.deletedConfiguration != "public-id" {
		t.Fatalf("deleted configuration = %q, want public-id", store.deletedConfiguration)
	}
	if got := []string{usageRecorder.events[0].Status, usageRecorder.events[1].Status}; !reflect.DeepEqual(got, []string{"terminating", "terminated"}) {
		t.Fatalf("usage statuses = %v, want [terminating terminated]", got)
	}
}

// Callers of the former force=true escape hatch keep working: an unreachable
// manager's route is now finalized locally regardless of the parameter.
func TestForceDeleteOrphanedRemoteSessionRemovesLocalRoute(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-gone",
		UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	store := &allocationReader{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(store),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id?force=true", "public-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatalf("force delete returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !routeRepo.deleted {
		t.Fatal("orphaned session route was not deleted")
	}
	if store.deletedConfiguration != "public-id" {
		t.Fatalf("deleted configuration = %q, want public-id", store.deletedConfiguration)
	}
}

// force=true must still enforce ownership: a user cannot force-delete another user's
// orphaned route.
func TestForceDeleteOrphanedRemoteSessionRequiresAuthorization(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-gone",
		UserID: "user-2", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, _ := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id?force=true", "public-id")

	err := controller.DeleteSession(ctx)
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusForbidden {
		t.Fatalf("expected 403 HTTPError, got %#v", err)
	}
	if routeRepo.deleted {
		t.Fatal("route must not be deleted when the caller is not authorized")
	}
}

func TestSuspendRemoteSessionUpdatesOnlyAllocatedSessionCache(t *testing.T) {
	allocated := &fakeSession{id: "remote-id", status: "active", userID: "user-1", scope: entities.ScopeUser}
	unrelated := &fakeSession{id: "other-id", status: "active", userID: "user-1", scope: entities.ScopeUser}
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"remote-id": allocated,
		"other-id":  unrelated,
	}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(&allocationReader{allocation: &sessionrunnercore.Allocation{
			SessionID: "public-id", RuntimeToken: "runtime-token", Generation: 2,
			ProvisionSettings: []byte(`{"session":{"user_id":"user-1","scope":"user"},"parent_runtime":{"endpoint":"http://parent.internal:8080"}}`),
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/suspend", "public-id")
	requestCtx, cancelRequest := context.WithCancel(ctx.Request().Context())
	defer cancelRequest()
	ctx.SetRequest(ctx.Request().WithContext(requestCtx))
	tunnel.onDo = cancelRequest

	if err := controller.SuspendSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.ctxErr != nil || tunnel.reqErr != nil {
		t.Fatalf("suspend tunnel inherited caller cancellation: ctx=%v request=%v", tunnel.ctxErr, tunnel.reqErr)
	}
	if tunnel.path != "/api/v1/sessions/remote-id/suspend" {
		t.Fatalf("suspend path = %q, want allocated session path", tunnel.path)
	}
	var suspendSettings sessionsettings.SessionSettings
	if err := json.Unmarshal(tunnel.body, &suspendSettings); err != nil {
		t.Fatalf("suspend body is invalid: %v; body=%s", err, tunnel.body)
	}
	if suspendSettings.ParentRuntime == nil || suspendSettings.ParentRuntime.Endpoint != "http://parent.internal:8080" || suspendSettings.ParentRuntime.Token != "runtime-token" || suspendSettings.ParentRuntime.Generation != 2 {
		t.Fatalf("suspend parent runtime = %#v, want persisted endpoint with refreshed allocation credentials", suspendSettings.ParentRuntime)
	}
	if !routeRepo.saved || routeRepo.route.Status != "suspended" || routeRepo.route.StatusUpdatedAt.IsZero() {
		t.Fatalf("route status was not persisted: %#v", routeRepo.route)
	}
	if allocated.status != "suspended" {
		t.Fatalf("allocated cache status = %q, want suspended", allocated.status)
	}
	if unrelated.status != "active" {
		t.Fatalf("unrelated cache status = %q, want active", unrelated.status)
	}
}

func TestSuspendRemoteSessionTreatsConcurrentCompletionAsSuccess(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{status: http.StatusServiceUnavailable}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser), Status: "active",
	}}
	tunnel.onDo = func() { routeRepo.route.Status = "suspended" }
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(&allocationReader{allocation: &sessionrunnercore.Allocation{
			SessionID: "public-id", RuntimeToken: "runtime-token", Generation: 2,
			ProvisionSettings: []byte(`{"session":{"user_id":"user-1","scope":"user"}}`),
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/suspend", "public-id")

	if err := controller.SuspendSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"suspended"`) {
		t.Fatalf("status=%d body=%s, want idempotent suspended response", rec.Code, rec.Body.String())
	}
}

func TestResumeRemoteSessionUsesSessionManagerAPIPath(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/resume", "public-id")

	if err := controller.ResumeSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.path != "/api/v1/sessions/remote-id/resume" {
		t.Fatalf("resume path = %q, want session manager API path", tunnel.path)
	}
}

func TestResumeRemoteSessionRefreshesAutoSuspendPolicy(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	settings := entities.NewSettings("user-1")
	settings.SetAutoSuspend(&entities.AutoSuspendSettings{Enabled: true, IdleTimeoutMinutes: 1})
	oldEnabled := true
	oldProvisionSettings, err := json.Marshal(&sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID: "user-1", Scope: string(entities.ScopeUser), AutoSuspendEnabled: &oldEnabled, AutoSuspendMinutes: 60,
		},
		ParentRuntime: &sessionsettings.ParentRuntimeConfig{Endpoint: "http://parent.internal:8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(&deletionRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
			UserID: "user-1", Scope: string(entities.ScopeUser),
		}}),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(&allocationReader{allocation: &sessionrunnercore.Allocation{ProvisionSettings: oldProvisionSettings, RuntimeToken: "runtime-token", Generation: 2}}),
		controllers.WithSettingsRepository(&resumeSettingsRepo{settings: settings}),
	)
	ctx, _ := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/resume", "public-id")
	if err := controller.ResumeSession(ctx); err != nil {
		t.Fatal(err)
	}
	var got sessionsettings.SessionSettings
	if err := json.Unmarshal(tunnel.body, &got); err != nil {
		t.Fatalf("resume body is invalid: %v; body=%s", err, tunnel.body)
	}
	if got.Session.AutoSuspendEnabled == nil || !*got.Session.AutoSuspendEnabled || got.Session.AutoSuspendMinutes != 1 {
		t.Fatalf("resume auto-suspend policy = enabled %v minutes %d, want true/1", got.Session.AutoSuspendEnabled, got.Session.AutoSuspendMinutes)
	}
	if got.ParentRuntime == nil || !got.ParentRuntime.Enabled || got.ParentRuntime.Endpoint != "http://parent.internal:8080" || got.ParentRuntime.SessionID != "public-id" || got.ParentRuntime.ManagerID != "manager-a" || got.ParentRuntime.Token != "runtime-token" || got.ParentRuntime.Generation != 2 {
		t.Fatalf("resume parent runtime = %#v, want restored allocation credentials", got.ParentRuntime)
	}
}
