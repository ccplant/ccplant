package controllers_test

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type reconcileRouteRepo struct {
	routes  map[string]*repositories.SessionRoute
	deleted []string
}

func (r *reconcileRouteRepo) Save(_ context.Context, route *repositories.SessionRoute) error {
	r.routes[route.SessionID] = route
	return nil
}

func (r *reconcileRouteRepo) Get(_ context.Context, id string) (*repositories.SessionRoute, error) {
	return r.routes[id], nil
}

func (r *reconcileRouteRepo) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	out := make([]*repositories.SessionRoute, 0, len(r.routes))
	for _, route := range r.routes {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out, nil
}

func (r *reconcileRouteRepo) Delete(_ context.Context, id string) error {
	delete(r.routes, id)
	r.deleted = append(r.deleted, id)
	return nil
}

type reconcileRunnerStore struct {
	managers       map[string]*sessionrunnercore.Manager
	runners        map[string]*sessionrunnercore.Runner
	allocations    map[string]*sessionrunnercore.Allocation
	configurations []sessionrunnercore.ConfigurationRef
	deletedConfigs []string
}

func (s *reconcileRunnerStore) GetAllocation(_ context.Context, id string) (*sessionrunnercore.Allocation, error) {
	if a, ok := s.allocations[id]; ok {
		return a, nil
	}
	return nil, sessionrunnercore.ErrNotFound
}

func (s *reconcileRunnerStore) ListAllocations(context.Context, string) ([]*sessionrunnercore.Allocation, error) {
	out := make([]*sessionrunnercore.Allocation, 0, len(s.allocations))
	for _, a := range s.allocations {
		out = append(out, a)
	}
	return out, nil
}

func (s *reconcileRunnerStore) GetManager(_ context.Context, id string) (*sessionrunnercore.Manager, error) {
	if m, ok := s.managers[id]; ok {
		return m, nil
	}
	return &sessionrunnercore.Manager{}, sessionrunnercore.ErrNotFound
}

func (s *reconcileRunnerStore) GetRunner(_ context.Context, id string) (*sessionrunnercore.Runner, error) {
	if r, ok := s.runners[id]; ok {
		return r, nil
	}
	return &sessionrunnercore.Runner{}, sessionrunnercore.ErrNotFound
}

func (s *reconcileRunnerStore) ListConfigurationRefs(context.Context) ([]sessionrunnercore.ConfigurationRef, error) {
	return s.configurations, nil
}

func (s *reconcileRunnerStore) DeleteConfiguration(_ context.Context, id string) error {
	s.deletedConfigs = append(s.deletedConfigs, id)
	return nil
}

// newReconcileFixture models the production failure: routes whose manager or
// runner record was deleted, next to sessions that must survive reconciliation.
// lifecycleTunnel reports only "manager-a" as connected.
func newReconcileFixture(now time.Time) (*reconcileRouteRepo, *reconcileRunnerStore) {
	old := now.Add(-2 * time.Hour)
	route := func(id, manager, status string, last time.Time) *repositories.SessionRoute {
		return &repositories.SessionRoute{
			SessionID: id, RemoteSessionID: "remote-" + id, ManagerID: manager, Status: status, StatusUpdatedAt: last,
			Transport: repositories.SessionRouteTransportDirectRuntime, UserID: "user-9", Scope: string(entities.ScopeUser),
		}
	}
	routes := &reconcileRouteRepo{routes: map[string]*repositories.SessionRoute{
		"manager-deleted": route("manager-deleted", "manager-gone", "running", old),
		"runner-deleted":  route("runner-deleted", "manager-b", "running", old),
		// DeleteAdminRunner removes the runner and its allocation; the route used
		// to survive with nothing left that names its owner.
		"runner-and-allocation-deleted": route("runner-and-allocation-deleted", "manager-b", "running", old),
		"recent":                        route("recent", "manager-gone", "running", now.Add(-time.Minute)),
		"suspended":                     route("suspended", "manager-b", "suspended", old),
		"healthy":                       route("healthy", "manager-a", "running", old),
		// The stored heartbeat is not written when liveness lives in Redis, so a
		// month-old value on a disconnected-looking manager must not end sessions.
		"stale-heartbeat":  route("stale-heartbeat", "manager-c", "running", old),
		"stuck-terminated": route("stuck-terminated", "manager-a", "terminating", old),
	}}
	store := &reconcileRunnerStore{
		managers: map[string]*sessionrunnercore.Manager{
			"manager-a": {ID: "manager-a", LastHeartbeatAt: now.Add(-time.Minute)},
			"manager-b": {ID: "manager-b", LastHeartbeatAt: now.Add(-time.Minute)},
			"manager-c": {ID: "manager-c", LastHeartbeatAt: now.Add(-30 * 24 * time.Hour)},
		},
		runners: map[string]*sessionrunnercore.Runner{
			"runner-healthy": {ID: "runner-healthy", ManagerID: "manager-a"},
			"runner-c":       {ID: "runner-c", ManagerID: "manager-c"},
		},
		allocations: map[string]*sessionrunnercore.Allocation{
			"runner-deleted":  {SessionID: "runner-deleted", ManagerID: "manager-b", RunnerID: "runner-removed"},
			"suspended":       {SessionID: "suspended", ManagerID: "manager-b", RunnerID: "runner-removed"},
			"healthy":         {SessionID: "healthy", ManagerID: "manager-a", RunnerID: "runner-healthy"},
			"stale-heartbeat": {SessionID: "stale-heartbeat", ManagerID: "manager-c", RunnerID: "runner-c"},
		},
		configurations: []sessionrunnercore.ConfigurationRef{
			{SessionID: "orphan", UpdatedAt: now.Add(-48 * time.Hour)},
			{SessionID: "orphan-but-recent", UpdatedAt: now.Add(-30 * time.Minute)},
			{SessionID: "owned-by-kubernetes", UpdatedAt: now.Add(-48 * time.Hour), HasOwner: true},
			{SessionID: "healthy", UpdatedAt: now.Add(-48 * time.Hour)},
			{SessionID: "local-session", UpdatedAt: now.Add(-48 * time.Hour)},
		},
	}
	return routes, store
}

func newReconcileController(routes *reconcileRouteRepo, store *reconcileRunnerStore) *controllers.SessionController {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{
		"local-session": {id: "local-session", userID: "user-9", scope: entities.ScopeUser, status: "active"},
	}}
	return controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routes),
		controllers.WithESMControlTunnel(&lifecycleTunnel{}),
		controllers.WithSessionRunnerStore(store),
	)
}

func TestReconcileSessionRoutesFinalizesAbandonedRoutesOnly(t *testing.T) {
	now := time.Now()
	routes, store := newReconcileFixture(now)
	controller := newReconcileController(routes, store)

	result := controller.ReconcileSessionRoutes(context.Background(), controllers.SessionRouteReconcileOptions{Now: func() time.Time { return now }})

	sort.Strings(routes.deleted)
	if want := []string{"manager-deleted", "runner-and-allocation-deleted", "runner-deleted", "stuck-terminated"}; !reflect.DeepEqual(routes.deleted, want) {
		t.Fatalf("deleted routes = %v, want %v", routes.deleted, want)
	}
	for _, kept := range []string{"recent", "suspended", "healthy", "stale-heartbeat"} {
		if routes.routes[kept] == nil {
			t.Fatalf("route %q must be kept", kept)
		}
	}
	wantReasons := map[string]int{
		controllers.RouteReconcileManagerDeleted: 1,
		controllers.RouteReconcileRunnerDeleted:  2,
		controllers.RouteReconcileStuckDeletion:  1,
	}
	if !reflect.DeepEqual(result.Finalized, wantReasons) || result.Failed != 0 {
		t.Fatalf("result = %#v, want finalized %v", result, wantReasons)
	}
	// Finalized sessions lose their configuration with the route; the only
	// other deletion is the configuration nothing references anymore.
	sort.Strings(store.deletedConfigs)
	if want := []string{"manager-deleted", "orphan", "runner-and-allocation-deleted", "runner-deleted", "stuck-terminated"}; !reflect.DeepEqual(store.deletedConfigs, want) {
		t.Fatalf("deleted configurations = %v, want %v", store.deletedConfigs, want)
	}
	if result.ConfigurationsDeleted != 1 {
		t.Fatalf("orphaned configurations deleted = %d, want 1", result.ConfigurationsDeleted)
	}
}

func TestReconcileSessionRoutesDryRunChangesNothing(t *testing.T) {
	now := time.Now()
	routes, store := newReconcileFixture(now)
	controller := newReconcileController(routes, store)

	result := controller.ReconcileSessionRoutes(context.Background(), controllers.SessionRouteReconcileOptions{DryRun: true, Now: func() time.Time { return now }})

	if len(routes.deleted) != 0 || len(store.deletedConfigs) != 0 {
		t.Fatalf("dry run deleted routes=%v configurations=%v", routes.deleted, store.deletedConfigs)
	}
	if total := result.Finalized[controllers.RouteReconcileManagerDeleted] + result.Finalized[controllers.RouteReconcileRunnerDeleted] + result.Finalized[controllers.RouteReconcileStuckDeletion]; total != 4 || result.ConfigurationsDeleted != 1 {
		t.Fatalf("dry run plan = %#v, want 4 routes and 1 configuration", result)
	}
}

// Without a complete allocation view a live session's configuration could look
// orphaned, so configuration GC must not run.
func TestReconcileSessionRoutesSkipsConfigurationGCWithoutAllocationView(t *testing.T) {
	now := time.Now()
	routes := &reconcileRouteRepo{routes: map[string]*repositories.SessionRoute{}}
	store := &reconcileRunnerStore{configurations: []sessionrunnercore.ConfigurationRef{{SessionID: "orphan", UpdatedAt: now.Add(-48 * time.Hour)}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routes),
		controllers.WithSessionRunnerStore(allocationOnlyStore{store}),
	)

	result := controller.ReconcileSessionRoutes(context.Background(), controllers.SessionRouteReconcileOptions{Now: func() time.Time { return now }})

	if result.ConfigurationsDeleted != 0 || len(store.deletedConfigs) != 0 {
		t.Fatalf("configuration GC ran without allocation listing: %v", store.deletedConfigs)
	}
}

type probeResponse struct {
	code int
	body string
}

// probeTunnel models control connections per connection ID (a direct runtime's
// public session ID, or a manager ID) and the runtime's answer per session.
type probeTunnel struct {
	connected map[string]bool
	responses map[string]probeResponse
	requests  []string
}

func (t *probeTunnel) IsConnected(_ context.Context, id string) bool { return t.connected[id] }

func (t *probeTunnel) Do(_ context.Context, _, sessionID, _ string, req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.Method+" "+sessionID+" "+req.URL.Host+req.URL.Path)
	r, ok := t.responses[sessionID]
	if !ok {
		r = probeResponse{code: http.StatusOK, body: `{}`}
	}
	return &http.Response{StatusCode: r.code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(r.body))}, nil
}

// A route that looks frozen is decided by its runtime, never by its age: a
// lost completion is healed, a long turn is kept, and only a runtime that stays
// gone (or a workload the manager disowns) is reclaimed.
func TestReconcileSessionRoutesDecidesFrozenRoutesByLiveStatus(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	ttl := map[string]string{"session_ttl": "1m"}
	direct := func(id string, tags map[string]string) *repositories.SessionRoute {
		return &repositories.SessionRoute{
			SessionID: id, RemoteSessionID: "runner-" + id, ManagerID: "manager-direct", Status: "running", StatusUpdatedAt: old,
			Transport: repositories.SessionRouteTransportDirectRuntime, UserID: "user-9", Scope: string(entities.ScopeUser), Tags: tags,
		}
	}
	routes := &reconcileRouteRepo{routes: map[string]*repositories.SessionRoute{
		"completion-lost":     direct("completion-lost", ttl),
		"long-turn":           direct("long-turn", ttl),
		"runtime-gone":        direct("runtime-gone", ttl),
		"runtime-gone-no-ttl": direct("runtime-gone-no-ttl", nil),
		"flapping":            direct("flapping", ttl),
		"workload-gone": {
			SessionID: "workload-gone", RemoteSessionID: "remote-workload-gone", ManagerID: "manager-esm", Status: "running", StatusUpdatedAt: old,
			Transport: repositories.SessionRouteTransportESMTunnel, UserID: "user-9", Scope: string(entities.ScopeUser), Tags: ttl,
		},
	}}
	store := &reconcileRunnerStore{
		managers:    map[string]*sessionrunnercore.Manager{"manager-direct": {ID: "manager-direct"}, "manager-esm": {ID: "manager-esm"}},
		runners:     map[string]*sessionrunnercore.Runner{},
		allocations: map[string]*sessionrunnercore.Allocation{},
	}
	for id := range routes.routes {
		// Runner records still exist: only the live runtime can tell these apart.
		store.runners["runner-"+id] = &sessionrunnercore.Runner{ID: "runner-" + id}
		store.allocations[id] = &sessionrunnercore.Allocation{SessionID: id, RunnerID: "runner-" + id}
	}
	tunnel := &probeTunnel{
		connected: map[string]bool{"completion-lost": true, "long-turn": true, "manager-esm": true},
		responses: map[string]probeResponse{
			"completion-lost": {code: http.StatusOK, body: `{"status":"stable"}`},
			"long-turn":       {code: http.StatusOK, body: `{"status":"running"}`},
			"workload-gone":   {code: http.StatusNotFound, body: `{"error":"not found"}`},
		},
	}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routes),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(store),
	)
	pass := func(at time.Time) controllers.SessionRouteReconcileResult {
		return controller.ReconcileSessionRoutes(context.Background(), controllers.SessionRouteReconcileOptions{Now: func() time.Time { return at }})
	}

	first := pass(now)
	if !reflect.DeepEqual(routes.deleted, []string{"workload-gone"}) {
		t.Fatalf("first pass deleted %v, want only the workload the manager disowned", routes.deleted)
	}
	if got := routes.routes["completion-lost"].Status; got != "active" || first.StatusesRefreshed != 1 {
		t.Fatalf("lost completion not healed: status=%q refreshed=%d", got, first.StatusesRefreshed)
	}
	if r := routes.routes["long-turn"]; r.Status != "running" || !r.StatusUpdatedAt.Equal(old) {
		t.Fatalf("long turn must be left running with its timestamp: %#v", r)
	}

	// The runtime reconnects briefly; its disconnect clock must restart.
	tunnel.connected["flapping"] = true
	tunnel.responses["flapping"] = probeResponse{code: http.StatusOK, body: `{"status":"running"}`}
	pass(now.Add(5 * time.Minute))
	tunnel.connected["flapping"] = false

	third := pass(now.Add(11 * time.Minute))
	sort.Strings(routes.deleted)
	if want := []string{"runtime-gone", "workload-gone"}; !reflect.DeepEqual(routes.deleted, want) {
		t.Fatalf("after the disconnect grace deleted %v, want %v", routes.deleted, want)
	}
	if third.Finalized[controllers.RouteReconcileRuntimeLost] != 1 {
		t.Fatalf("third pass = %#v, want one runtime_lost", third)
	}
	for _, kept := range []string{"completion-lost", "long-turn", "runtime-gone-no-ttl", "flapping"} {
		if routes.routes[kept] == nil {
			t.Fatalf("route %q must be kept", kept)
		}
	}
}

// allocationOnlyStore hides ListAllocations while still exposing configurations.
type allocationOnlyStore struct{ store *reconcileRunnerStore }

func (s allocationOnlyStore) GetAllocation(ctx context.Context, id string) (*sessionrunnercore.Allocation, error) {
	return s.store.GetAllocation(ctx, id)
}
func (s allocationOnlyStore) ListConfigurationRefs(ctx context.Context) ([]sessionrunnercore.ConfigurationRef, error) {
	return s.store.ListConfigurationRefs(ctx)
}
func (s allocationOnlyStore) DeleteConfiguration(ctx context.Context, id string) error {
	return s.store.DeleteConfiguration(ctx, id)
}

// DELETE must not strand a route whose manager is unreachable: the manager reaps
// the workload from orphaned_session_ids when it reconnects.
func TestDeleteRemoteSessionFinalizesLocallyWhenManagerUnreachable(t *testing.T) {
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-gone",
		Transport: repositories.SessionRouteTransportDirectRuntime,
		UserID:    "user-1", Scope: string(entities.ScopeUser),
	}}
	store := &allocationReader{}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
		controllers.WithSessionRunnerStore(store),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	if rec.Code != http.StatusOK || !routeRepo.deleted {
		t.Fatalf("status=%d route deleted=%t, want 200 and deleted", rec.Code, routeRepo.deleted)
	}
	if store.deletedConfiguration != "public-id" {
		t.Fatalf("deleted configuration = %q, want public-id", store.deletedConfiguration)
	}
	if tunnel.path != "" || tunnel.enqueued {
		t.Fatalf("unreachable manager must not be contacted: path=%q enqueued=%t", tunnel.path, tunnel.enqueued)
	}
}

// Routed sessions bypass the managed-session ownership check, so the remote
// path itself must refuse other users before contacting the manager.
func TestDeleteRemoteSessionRejectsNonOwner(t *testing.T) {
	tunnel := &lifecycleTunnel{}
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-a",
		UserID: "user-2", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, _ := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	err := controller.DeleteSession(ctx)
	if he, ok := err.(*echo.HTTPError); !ok || he.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %#v", err)
	}
	if routeRepo.deleted || tunnel.path != "" {
		t.Fatalf("non-owner delete reached storage or manager: deleted=%t path=%q", routeRepo.deleted, tunnel.path)
	}
}

func TestDeleteRemoteSessionFromWorkerBypassesOwnership(t *testing.T) {
	routeRepo := &deletionRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "runner-id", ManagerID: "manager-gone",
		UserID: "user-2", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(&lifecycleTunnel{}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/public-id", "public-id")

	if err := controller.DeleteSessionFromWorker(ctx); err != nil {
		t.Fatalf("worker delete returned error: %v", err)
	}
	if rec.Code != http.StatusOK || !routeRepo.deleted {
		t.Fatalf("status=%d deleted=%t, want worker cleanup to finalize", rec.Code, routeRepo.deleted)
	}
}
