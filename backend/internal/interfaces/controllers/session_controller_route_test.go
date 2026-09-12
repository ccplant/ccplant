package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type ensuringSessionManager struct {
	*fakeSessionManager
	ensuredIDs []string
	restoring  bool
}

func (m *ensuringSessionManager) EnsureSessionWorkload(_ context.Context, id string) (entities.Session, bool, error) {
	m.ensuredIDs = append(m.ensuredIDs, id)
	return m.GetSession(id), m.restoring, nil
}

type routeSessionManagerProvider struct {
	manager repositories.SessionManager
}

type statusWatchingSessionManager struct {
	*fakeSessionManager
	events chan repositories.SessionStatusEvent
}

func (m *statusWatchingSessionManager) SubscribeStatusEvents() (<-chan repositories.SessionStatusEvent, func()) {
	return m.events, func() {}
}

type directRuntimeTunnel struct {
	managerID string
	path      string
}

type lifecycleTunnel struct {
	path     string
	body     []byte
	enqueued bool
	done     bool
	status   int
}

func (t *lifecycleTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "manager-a"
}

func (t *lifecycleTunnel) Do(_ context.Context, _, _, _ string, req *http.Request) (*http.Response, error) {
	t.path = req.URL.Path
	if req.Body != nil {
		t.body, _ = io.ReadAll(req.Body)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

type allocationReader struct {
	allocation *sessionrunnercore.Allocation
	err        error
}

func (s *allocationReader) GetAllocation(context.Context, string) (*sessionrunnercore.Allocation, error) {
	return s.allocation, s.err
}

func (t *lifecycleTunnel) Enqueue(_ context.Context, _, _, _ string, req *http.Request) (string, error) {
	t.path = req.URL.Path
	t.enqueued = true
	return "request-id", nil
}

func (t *lifecycleTunnel) CommandResult(_ context.Context, _ string) (bool, int, error) {
	return t.done, t.status, nil
}

type deletionRouteRepo struct {
	route   *repositories.SessionRoute
	saved   bool
	deleted bool
}

func (r *deletionRouteRepo) Save(_ context.Context, route *repositories.SessionRoute) error {
	r.route = route
	r.saved = true
	return nil
}
func (r *deletionRouteRepo) Get(_ context.Context, sessionID string) (*repositories.SessionRoute, error) {
	if r.route != nil && r.route.SessionID == sessionID {
		return r.route, nil
	}
	return nil, nil
}
func (r *deletionRouteRepo) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	if r.route == nil {
		return nil, nil
	}
	return []*repositories.SessionRoute{r.route}, nil
}
func (r *deletionRouteRepo) Delete(context.Context, string) error {
	r.deleted = true
	r.route = nil
	return nil
}

func (t *directRuntimeTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "public-id"
}

func (t *directRuntimeTunnel) Do(_ context.Context, managerID, _, _ string, req *http.Request) (*http.Response, error) {
	t.managerID = managerID
	t.path = req.URL.Path
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":"stable"}`)),
	}, nil
}

func (p *routeSessionManagerProvider) GetSessionManager() repositories.SessionManager {
	return p.manager
}

func routeContext(e *echo.Echo, method, path, sessionID string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId", "*")
	ctx.SetParamValues(sessionID, strings.TrimPrefix(path, "/"+sessionID+"/"))
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
	})
	return ctx, rec
}

func TestRouteToSessionEnsuresLocalAliasWorkloadOnGet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("upstream path = %q, want /status", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"stable"}`))
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"remote-id": {id: "remote-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id",
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "remote-id" {
		t.Fatalf("ensured IDs = %v, want [remote-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionEnsuresRegularLocalWorkloadOnGet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"local-id": {id: "local-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/local-id/status", "local-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "local-id" {
		t.Fatalf("ensured IDs = %v, want [local-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionReturnsStructuredResumingResponse(t *testing.T) {
	manager := &ensuringSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
			"local-id": {id: "local-id", userID: "user-1", scope: entities.ScopeUser},
		}},
		restoring: true,
	}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/local-id/status", "local-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d retry-after=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"session_resuming"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

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
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
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
}

func TestDeleteDirectRuntimeUsesAllocatedRunnerID(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "allocated-runner-id", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime,
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
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
			ProvisionSettings: []byte(`{"session":{"user_id":"user-1","scope":"user"}}`),
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/suspend", "public-id")

	if err := controller.SuspendSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.path != "/api/v1/sessions/remote-id/suspend" {
		t.Fatalf("suspend path = %q, want allocated session path", tunnel.path)
	}
	if !strings.Contains(string(tunnel.body), `"token":"runtime-token"`) || !strings.Contains(string(tunnel.body), `"generation":2`) {
		t.Fatalf("suspend body does not contain resume data: %s", tunnel.body)
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

func TestRouteToSuspendedRemoteSessionTransparentlyStartsResume(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		UserID: "user-1", Scope: string(entities.ScopeUser), Status: "suspended",
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

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
