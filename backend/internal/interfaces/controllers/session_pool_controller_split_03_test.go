package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionManagerHeartbeatRetiresStaleIdleRunners(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRunner(ctx, &core.Runner{ID: "stale", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(store, nil)
	controller.now = func() time.Time { return time.Now().UTC().Add(sessionRunnerHeartbeatTTL + time.Minute) }

	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	if runners, err := store.ListRunners(ctx, "linux"); err != nil || len(runners) != 1 || runners[0].Status != core.RunnerDraining {
		t.Fatalf("stale runner must remain fenced: runners=%+v err=%v", runners, err)
	}
	var heartbeat struct {
		Pools []*core.PoolSupplier `json:"pools"`
	}
	decodeRecorder(t, result, &heartbeat)
	if len(heartbeat.Pools) != 1 || heartbeat.Pools[0].TotalRunners != 0 || heartbeat.Pools[0].IdleRunners != 0 {
		t.Fatalf("unexpected heartbeat pools: %+v", heartbeat.Pools)
	}
}

func TestSessionManagerHeartbeatDisablesSupplierForDisabledLogicalPool(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true, MinIdle: 2}); err != nil {
		t.Fatal(err)
	}

	controller := NewSessionPoolController(store, nil)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	var heartbeat struct {
		Pools []*core.PoolSupplier `json:"pools"`
	}
	decodeRecorder(t, result, &heartbeat)
	if len(heartbeat.Pools) != 1 || heartbeat.Pools[0].Enabled {
		t.Fatalf("disabled logical pool remained enabled in heartbeat: %+v", heartbeat.Pools)
	}
}

func TestSessionManagerHeartbeatReportsAllocatedRunnerIDs(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, runner := range []*core.Runner{
		{ID: "idle-runner", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle},
		{ID: "allocated-runner", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerRunning},
	} {
		if err := store.CreateRunner(ctx, runner); err != nil {
			t.Fatal(err)
		}
	}
	controller := NewSessionPoolController(store, nil)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	var heartbeat struct {
		AllocatedRunnerIDs []string `json:"allocated_runner_ids"`
	}
	decodeRecorder(t, result, &heartbeat)
	if len(heartbeat.AllocatedRunnerIDs) != 1 || heartbeat.AllocatedRunnerIDs[0] != "allocated-runner" {
		t.Fatalf("allocated_runner_ids = %v", heartbeat.AllocatedRunnerIDs)
	}
}

func TestSessionManagerHeartbeatReportsPoliciesOnlyForActiveAllocations(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"active-runner", "missing-active-runner", "inactive-runner"} {
		if err := store.CreateRunner(ctx, &core.Runner{ID: id, ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle}); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := json.Marshal(sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{AgentType: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, &core.Allocation{SessionID: "active-session", Pool: "linux", ProvisionSettings: settings}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimNext(ctx, "linux", "active-runner", time.Minute); err != nil || !found {
		t.Fatalf("claim active allocation: found=%v err=%v", found, err)
	}
	if err := store.Enqueue(ctx, &core.Allocation{SessionID: "missing-active-session", Pool: "linux", ProvisionSettings: settings}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ClaimNext(ctx, "linux", "missing-active-runner", time.Minute); err != nil || !found {
		t.Fatalf("claim missing active allocation: found=%v err=%v", found, err)
	}
	if err := store.Enqueue(ctx, &core.Allocation{
		SessionID: "inactive-session", Pool: "linux", ManagerID: manager.ID,
		RunnerID: "inactive-runner", ProvisionSettings: settings,
	}); err != nil {
		t.Fatal(err)
	}

	controller := NewSessionPoolController(store, nil)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"local_runner_ids": []string{"active-runner", "inactive-runner"}}, map[string]string{"id": manager.ID},
		map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	var heartbeat struct {
		AllocatedRunnerPolicies map[string]sessionsettings.SessionMeta `json:"allocated_runner_policies"`
	}
	decodeRecorder(t, result, &heartbeat)
	if _, ok := heartbeat.AllocatedRunnerPolicies["active-runner"]; !ok {
		t.Fatalf("active runner policy missing: %v", heartbeat.AllocatedRunnerPolicies)
	}
	if _, ok := heartbeat.AllocatedRunnerPolicies["inactive-runner"]; ok {
		t.Fatalf("inactive runner policy was reported: %v", heartbeat.AllocatedRunnerPolicies)
	}
	if _, ok := heartbeat.AllocatedRunnerPolicies["missing-active-runner"]; ok {
		t.Fatalf("missing active runner policy was reported: %v", heartbeat.AllocatedRunnerPolicies)
	}
}

func TestSessionManagerHeartbeatReconcilesSuspendedSessionStatus(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	route := &portrepos.SessionRoute{SessionID: "public-a", RemoteSessionID: "remote-a", ManagerID: manager.ID, Status: "active"}
	if err := routes.Save(ctx, route); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(store, routes)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"session_statuses": map[string]string{"remote-a": "suspended"}}, map[string]string{"id": manager.ID},
		map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	updated, err := routes.Get(ctx, "public-a")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "suspended" || updated.StatusUpdatedAt.IsZero() {
		t.Fatalf("route status=%q updated_at=%v", updated.Status, updated.StatusUpdatedAt)
	}
}

func TestSessionManagerHeartbeatReturnsOnlySessionsAbsentFromParentRoutes(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	token, tokenHash, err := newSessionRunnerToken()
	requireNoError(t, err)
	manager := &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: tokenHash}
	requireNoError(t, store.CreateManager(ctx, manager))
	requireNoError(t, routes.Save(ctx, &portrepos.SessionRoute{
		SessionID: "public-a", RemoteSessionID: "remote-a", ManagerID: manager.ID, Status: "active",
	}))
	requireNoError(t, routes.Save(ctx, &portrepos.SessionRoute{
		SessionID: "other", RemoteSessionID: "other-remote", ManagerID: "manager-b", Status: "active",
	}))

	result := callSessionPoolHandler(t, NewSessionPoolController(store, routes).HeartbeatManager,
		http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"session_statuses": map[string]string{
			"public-a": "running", "remote-a": "running", "other": "running", "orphan-b": "running", "orphan-a": "error",
		}}, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	var response struct {
		OrphanedSessionIDs []string `json:"orphaned_session_ids"`
	}
	decodeRecorder(t, result, &response)
	if got, want := response.OrphanedSessionIDs, []string{"orphan-a", "orphan-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("orphaned_session_ids=%v, want %v", got, want)
	}
}

func TestSessionManagerHeartbeatDoesNotOverwriteSuspendedRouteWithStopped(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	statusUpdatedAt := time.Now().Add(-time.Minute)
	route := &portrepos.SessionRoute{
		SessionID: "public-a", RemoteSessionID: "remote-a", ManagerID: manager.ID,
		Status: "suspended", StatusUpdatedAt: statusUpdatedAt,
	}
	if err := routes.Save(ctx, route); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(store, routes)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"session_statuses": map[string]string{"remote-a": "stopped"}}, map[string]string{"id": manager.ID},
		map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	updated, err := routes.Get(ctx, "public-a")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "suspended" || !updated.StatusUpdatedAt.Equal(statusUpdatedAt) {
		t.Fatalf("route status=%q updated_at=%v, want suspended at %v", updated.Status, updated.StatusUpdatedAt, statusUpdatedAt)
	}
}

func TestSessionManagerHeartbeatDoesNotOverwriteResumingRouteWithStopped(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	route := &portrepos.SessionRoute{SessionID: "public-a", RemoteSessionID: "remote-a", ManagerID: manager.ID, Status: "resuming"}
	if err := routes.Save(ctx, route); err != nil {
		t.Fatal(err)
	}
	controller := NewSessionPoolController(store, routes)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"session_statuses": map[string]string{"remote-a": "stopped"}}, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	updated, err := routes.Get(ctx, "public-a")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "resuming" {
		t.Fatalf("route status=%q, want resuming", updated.Status)
	}
}

func TestSessionManagerHeartbeatPreservesAllocatedRunnersMissingFromLocalInventory(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, runner := range []*core.Runner{
		{ID: "live-running", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerRunning},
		{ID: "missing-running", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerRunning},
		{ID: "missing-idle", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle},
	} {
		if err := store.CreateRunner(ctx, runner); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Enqueue(ctx, &core.Allocation{SessionID: "stale-session", Pool: "linux", RunnerID: "missing-running"}); err != nil {
		t.Fatal(err)
	}

	controller := NewSessionPoolController(store, nil)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"local_runner_ids": []string{"live-running", "missing-idle"}}, map[string]string{"id": manager.ID},
		map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	if _, err := store.GetRunner(ctx, "missing-running"); err != nil {
		t.Fatalf("missing running runner was deleted: %v", err)
	}
	if _, err := store.GetAllocation(ctx, "stale-session"); err != nil {
		t.Fatalf("allocation recovery data was deleted: %v", err)
	}
	for _, id := range []string{"live-running", "missing-idle"} {
		if _, err := store.GetRunner(ctx, id); err != nil {
			t.Fatalf("runner %s was unexpectedly deleted: %v", id, err)
		}
	}
}
