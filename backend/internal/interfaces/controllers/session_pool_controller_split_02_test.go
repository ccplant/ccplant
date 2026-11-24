package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeleteAdminRunnerDeletesManagerWorkloadAndParentRecords(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	ctx := context.Background()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerRunning}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: "manager-a", RunnerID: "runner-a"}))
	requireNoError(t, routes.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", RemoteSessionID: "runner-a", ManagerID: "manager-a"}))
	tunnel := &runnerInventoryTunnel{statusTestTunnel: statusTestTunnel{connected: map[string]bool{"manager-a": true}}}
	controller := NewSessionPoolController(store, routes).WithManagerTunnel(tunnel)

	rec := callSessionPoolHandler(t, controller.DeleteAdminRunner, http.MethodDelete, "/admin/session-runners/runner-a?manager_id=manager-a", nil, map[string]string{"id": "runner-a"}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetRunner(ctx, "runner-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("runner still exists: %v", err)
	}
	if _, err := store.GetAllocation(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("allocation still exists: %v", err)
	}
	if route, err := routes.Get(ctx, "session-a"); err != nil || route != nil {
		t.Fatalf("route still exists: route=%+v err=%v", route, err)
	}
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if len(tunnel.requests) != 1 || tunnel.requests[0] != "http://manager/internal/esm-management/runners/runner-a" {
		t.Fatalf("requests=%v", tunnel.requests)
	}
}

func TestDeleteAdminRunnerForceCleansOfflineManagerRecords(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	ctx := context.Background()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerRunning}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: "manager-a", RunnerID: "runner-a"}))
	requireNoError(t, routes.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", RemoteSessionID: "runner-a", ManagerID: "manager-a"}))
	tunnel := &runnerInventoryTunnel{statusTestTunnel: statusTestTunnel{connected: map[string]bool{"manager-a": false}}}
	controller := NewSessionPoolController(store, routes).WithManagerTunnel(tunnel)

	refused := callSessionPoolHandler(t, controller.DeleteAdminRunner, http.MethodDelete, "/admin/session-runners/runner-a?manager_id=manager-a", nil, map[string]string{"id": "runner-a"}, nil)
	if refused.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-force status=%d body=%s", refused.Code, refused.Body.String())
	}
	forced := callSessionPoolHandler(t, controller.DeleteAdminRunner, http.MethodDelete, "/admin/session-runners/runner-a?manager_id=manager-a&force=true", nil, map[string]string{"id": "runner-a"}, nil)
	if forced.Code != http.StatusNoContent {
		t.Fatalf("force status=%d body=%s", forced.Code, forced.Body.String())
	}
	if _, err := store.GetRunner(ctx, "runner-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("runner still exists: %v", err)
	}
	if _, err := store.GetAllocation(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("allocation still exists: %v", err)
	}
	if route, err := routes.Get(ctx, "session-a"); err != nil || route != nil {
		t.Fatalf("route still exists: route=%+v err=%v", route, err)
	}
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if len(tunnel.requests) != 0 {
		t.Fatalf("offline manager was contacted: requests=%v", tunnel.requests)
	}
}

// A retry after a partial delete finds no runner record. The allocation must
// still identify the session so its route and configuration are removed instead
// of surviving as an undeletable stale session.
func TestDeleteAdminRunnerCleansSessionWhenRunnerRecordIsAlreadyGone(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	ctx := context.Background()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: "manager-a", RunnerID: "runner-a"}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "other-manager", Pool: "linux", ManagerID: "manager-b", RunnerID: "runner-a"}))
	requireNoError(t, store.CreateConfiguration(ctx, &core.Configuration{SessionID: "session-a", Input: []byte(`{}`)}))
	requireNoError(t, routes.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", RemoteSessionID: "remote-session", ManagerID: "manager-a"}))
	tunnel := &runnerInventoryTunnel{statusTestTunnel: statusTestTunnel{connected: map[string]bool{"manager-a": false}}}
	controller := NewSessionPoolController(store, routes).WithManagerTunnel(tunnel)

	rec := callSessionPoolHandler(t, controller.DeleteAdminRunner, http.MethodDelete, "/admin/session-runners/runner-a?manager_id=manager-a&force=true", nil, map[string]string{"id": "runner-a"}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetAllocation(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("allocation still exists: %v", err)
	}
	if _, err := store.GetAllocation(ctx, "other-manager"); err != nil {
		t.Fatalf("allocation owned by another manager was removed: %v", err)
	}
	if route, err := routes.Get(ctx, "session-a"); err != nil || route != nil {
		t.Fatalf("route still exists: route=%+v err=%v", route, err)
	}
	if _, err := store.GetConfiguration(ctx, "session-a"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("configuration still exists: %v", err)
	}
}

type testManagerLiveness struct {
	connected map[string]bool
}

type countingRunnerStore struct {
	core.Store
	claimCalls atomic.Int32
}

func (s *countingRunnerStore) ClaimNext(ctx context.Context, pool, runnerID string, lease time.Duration) (*core.Allocation, bool, error) {
	s.claimCalls.Add(1)
	return s.Store.ClaimNext(ctx, pool, runnerID, lease)
}

func TestRunnerLongPollClaimsOnlyAfterPoolNotification(t *testing.T) {
	baseStore := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	store := &countingRunnerStore{Store: baseStore}
	notifier := infra.NewLocalAllocationNotifier()
	controller := NewSessionPoolController(store, nil).WithAllocationNotifier(notifier)
	now := time.Now().UTC()
	_, managerTokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	runnerToken, runnerTokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateManager(context.Background(), &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: managerTokenHash, LastHeartbeatAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRunner(context.Background(), &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "managed", TokenHash: runnerTokenHash, Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}

	resultCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		resultCh <- callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=2s", nil, nil,
			map[string]string{"Authorization": "Bearer " + runnerToken, "X-Session-Runner-ID": "runner-a"})
	}()

	deadline := time.Now().Add(time.Second)
	for store.claimCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := store.claimCalls.Load(); got != 1 {
		t.Fatalf("claim calls before notification = %d, want 1", got)
	}
	time.Sleep(50 * time.Millisecond)
	if got := store.claimCalls.Load(); got != 1 {
		t.Fatalf("long poll queried the store without a notification: calls=%d", got)
	}
	if err := notifier.Notify(context.Background(), "other-pool"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if got := store.claimCalls.Load(); got != 1 {
		t.Fatalf("notification for another pool triggered a claim: calls=%d", got)
	}
	if err := store.Enqueue(context.Background(), &core.Allocation{SessionID: "session-a", Pool: "managed"}); err != nil {
		t.Fatal(err)
	}
	if err := notifier.Notify(context.Background(), "managed"); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-resultCh:
		if result.Code != http.StatusOK {
			t.Fatalf("claim status=%d body=%s", result.Code, result.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not wake after allocation notification")
	}
	if got := store.claimCalls.Load(); got != 2 {
		t.Fatalf("claim calls after notification = %d, want 2", got)
	}
}

func TestSystemManagerRegistrationDoesNotConfigurePools(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	admin := entities.NewUser("admin-1", entities.UserTypeAdmin, "admin")
	requireNoError(t, admin.SetRoles([]entities.Role{entities.RoleAdmin}))

	issued := callSessionPoolHandlerAs(t, controller.IssueManagerRegistrationToken, http.MethodPost, "/session-managers/registration-tokens",
		map[string]any{"name": "kubernetes-dev", "scope": "system", "pool": "default", "default": true}, nil, nil, admin)
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", issued.Code, issued.Body.String())
	}
	var registration struct {
		Manager struct {
			ID string `json:"id"`
		} `json:"manager"`
		Token string `json:"registration_token"`
	}
	decodeRecorder(t, issued, &registration)
	if registration.Manager.ID == "" || registration.Token == "" {
		t.Fatalf("invalid registration response: %s", issued.Body.String())
	}

	enrolled := callSessionPoolHandler(t, controller.EnrollManager, http.MethodPost, "/session-managers/enroll",
		map[string]any{"registration_token": registration.Token, "instance_id": "dev/session-manager", "name": "Native build host"}, nil, nil)
	if enrolled.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", enrolled.Code, enrolled.Body.String())
	}
	var credentials struct {
		ID              string `json:"id"`
		ConnectionToken string `json:"connection_token"`
	}
	decodeRecorder(t, enrolled, &credentials)
	if credentials.ID != registration.Manager.ID || credentials.ConnectionToken == "" {
		t.Fatalf("invalid enrollment response: %s", enrolled.Body.String())
	}
	manager, err := store.GetManager(context.Background(), credentials.ID)
	if err != nil || manager.Name != "Native build host" {
		t.Fatalf("enrollment name = %q, err=%v", manager.Name, err)
	}
	if _, err := store.GetLogicalPool(context.Background(), "default"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("registration unexpectedly created a logical pool: %v", err)
	}
	if _, err := store.GetPoolSupplier(context.Background(), credentials.ID, "default"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("registration unexpectedly created a pool supplier: %v", err)
	}
	bindings, err := store.ListBindings(context.Background(), "default")
	if err != nil || len(bindings) != 0 {
		t.Fatalf("registration unexpectedly created pool bindings: bindings=%+v err=%v", bindings, err)
	}

	reused := callSessionPoolHandler(t, controller.EnrollManager, http.MethodPost, "/session-managers/enroll",
		map[string]any{"registration_token": registration.Token, "instance_id": "dev/session-manager"}, nil, nil)
	if reused.Code != http.StatusUnauthorized {
		t.Fatalf("registration token reuse status=%d body=%s", reused.Code, reused.Body.String())
	}
}

func TestCreatePoolSupplierRequiresManagerAuthorization(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	requireNoError(t, store.CreateManager(ctx, &core.Manager{
		ID: "manager-a", Name: "Manager A", Scope: core.ManagerScopeUser, OwnerID: "alice", Enabled: true,
	}))
	requireNoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}))
	requireNoError(t, store.CreateBinding(ctx, &core.Binding{
		Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "bob", Role: core.BindingRoleManage, Enabled: true,
	}))
	bob := entities.NewUser(entities.UserID("bob"), entities.UserTypeAPIKey, "bob")

	result := callSessionPoolHandlerAs(t, controller.CreatePoolSupplier, http.MethodPost, "/session-pools/linux/suppliers/manager-a",
		map[string]any{"pool": "linux"}, map[string]string{"pool": "linux", "id": "manager-a"}, nil, bob)

	if result.Code != http.StatusNotFound {
		t.Fatalf("create supplier status=%d body=%s", result.Code, result.Body.String())
	}
	if _, err := store.GetPoolSupplier(ctx, "manager-a", "linux"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("supplier unexpectedly created: %v", err)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (l *testManagerLiveness) TouchManager(_ context.Context, managerID, _ string) error {
	l.connected[managerID] = true
	return nil
}

func (l *testManagerLiveness) IsManagerConnected(_ context.Context, managerID string) (bool, error) {
	return l.connected[managerID], nil
}

func TestSessionManagerHeartbeatUsesSharedLivenessWithoutPersistingManager(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetManager(context.Background(), manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	liveness := &testManagerLiveness{connected: map[string]bool{}}
	controller := NewSessionPoolController(store, nil).WithManagerLiveness(liveness)

	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	after, err := store.GetManager(context.Background(), manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !liveness.connected[manager.ID] {
		t.Fatal("manager liveness was not refreshed")
	}
	if after.UpdatedAt != before.UpdatedAt || !after.LastHeartbeatAt.IsZero() {
		t.Fatalf("manager was unexpectedly persisted: before=%+v after=%+v", before, after)
	}
}

func TestSessionManagerHeartbeatResolvesCurrentManagerIDFromToken(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "generated-manager-id", Name: "Manager", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(store, nil)

	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/stale-manager-id/heartbeat",
		nil, map[string]string{"id": "stale-manager-id"}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	var heartbeat struct {
		ManagerID string `json:"manager_id"`
	}
	decodeRecorder(t, result, &heartbeat)
	if heartbeat.ManagerID != manager.ID {
		t.Fatalf("manager_id = %q, want %q", heartbeat.ManagerID, manager.ID)
	}
}

func TestSessionManagerHeartbeatCreatesDiagnosticSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		requireNoError(t, provider.Shutdown(context.Background()))
	})

	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	requireNoError(t, err)
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	requireNoError(t, store.CreateManager(context.Background(), manager))

	result := callSessionPoolHandler(t, NewSessionPoolController(store, nil).HeartbeatManager, http.MethodPost,
		"/internal/session-managers/manager-a/heartbeat", map[string]any{"local_runner_ids": []string{}},
		map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}

	spanNames := make(map[string]bool)
	for _, span := range recorder.Ended() {
		spanNames[span.Name()] = true
	}
	for _, want := range []string{
		"controllers.SessionPoolController.HeartbeatManager",
		"session_pool.heartbeat.update_manager_liveness",
		"session_pool.heartbeat.reconcile_missing_runners",
		"session_pool.heartbeat.find_orphaned_sessions",
		"session_pool.heartbeat.list_pool_suppliers",
		"session_pool.heartbeat.list_logical_pools",
		"session_pool.heartbeat.list_runners",
		"session_pool.heartbeat.list_manager_allocations",
		"session_pool.heartbeat.repair_manager_routes",
	} {
		if !spanNames[want] {
			t.Errorf("diagnostic span %q was not emitted; got %v", want, spanNames)
		}
	}
}
