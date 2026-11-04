package controllers_test

import (
	"context"
	"net/http"
	"reflect"
	"sort"
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
			{SessionID: "orphan-but-recent", UpdatedAt: now.Add(-time.Hour)},
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
