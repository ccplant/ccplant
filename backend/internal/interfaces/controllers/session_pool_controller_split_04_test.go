package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/buildinfo"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionManagerHeartbeatRepairsMissingRunnerRoute(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
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
	if err := store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux"}); err != nil {
		t.Fatal(err)
	}
	allocation, found, err := store.ClaimNext(ctx, "linux", "runner-a", time.Minute)
	if err != nil || !found {
		t.Fatalf("claim allocation: found=%v err=%v", found, err)
	}
	if _, err := store.Acknowledge(ctx, allocation.SessionID, "runner-a", allocation.LeaseID); err != nil {
		t.Fatal(err)
	}
	if err := routes.Save(ctx, &portrepos.SessionRoute{SessionID: "session-a", ManagerID: "manager-a"}); err != nil {
		t.Fatal(err)
	}

	controller := NewSessionPoolController(store, routes)
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	route, err := routes.Get(ctx, "session-a")
	if err != nil || route.RemoteSessionID != "runner-a" {
		t.Fatalf("route was not repaired: route=%+v err=%v", route, err)
	}
}

func TestRunnerPollRefreshesLastSeen(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	now := time.Now().UTC().Add(time.Hour)
	controller.now = func() time.Time { return now }
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", TokenHash: tokenHash, Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Session-Runner-ID", "runner-a")
	if _, err := controller.authenticateRunner(e.NewContext(req, httptest.NewRecorder())); err != nil {
		t.Fatal(err)
	}
	runner, err := store.GetRunner(ctx, "runner-a")
	if err != nil || !runner.LastSeen.Equal(now) {
		t.Fatalf("runner last seen was not refreshed: runner=%+v err=%v", runner, err)
	}
}

func TestSessionPoolRunnerClaimLifecycle(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	controller := NewSessionPoolController(store, routes)

	managerResult := callSessionPoolHandler(t, controller.CreateManager, http.MethodPost, "/admin/session-managers",
		map[string]any{"id": "manager-a", "name": "Manager A"}, nil, nil)
	if managerResult.Code != http.StatusCreated {
		t.Fatalf("create manager status=%d body=%s", managerResult.Code, managerResult.Body.String())
	}
	var created struct {
		ConnectionToken string `json:"connection_token"`
	}
	decodeRecorder(t, managerResult, &created)
	if created.ConnectionToken == "" {
		t.Fatal("manager connection token was not returned")
	}
	previousVersion := buildinfo.Version
	buildinfo.Version = "v2.4.0"
	t.Cleanup(func() { buildinfo.Version = previousVersion })
	heartbeatResult := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": "manager-a"}, map[string]string{"Authorization": "Bearer " + created.ConnectionToken})
	if heartbeatResult.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", heartbeatResult.Code, heartbeatResult.Body.String())
	}
	var heartbeat struct {
		UpstreamVersion string `json:"upstream_version"`
	}
	decodeRecorder(t, heartbeatResult, &heartbeat)
	if heartbeat.UpstreamVersion != "v2.4.0" {
		t.Fatalf("upstream_version = %q", heartbeat.UpstreamVersion)
	}
	if _, err := store.GetManager(context.Background(), "manager-a"); err != nil {
		t.Fatalf("created manager not persisted: %v, body=%s", err, managerResult.Body.String())
	}

	logicalPoolResult := callSessionPoolHandler(t, controller.CreateLogicalPool, http.MethodPost, "/admin/session-pools",
		map[string]any{"name": "linux"}, nil, nil)
	if logicalPoolResult.Code != http.StatusCreated {
		t.Fatalf("create logical pool status=%d body=%s", logicalPoolResult.Code, logicalPoolResult.Body.String())
	}
	poolResult := callSessionPoolHandler(t, controller.CreatePoolSupplier, http.MethodPost, "/admin/session-managers/manager-a/pools",
		map[string]any{"pool": "linux", "min_idle": 1, "max_runners": 2}, map[string]string{"id": "manager-a"}, nil)
	if poolResult.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", poolResult.Code, poolResult.Body.String())
	}
	pool, err := store.GetPoolSupplier(context.Background(), "manager-a", "linux")
	if err != nil || !pool.Enabled {
		t.Fatalf("pool should default enabled: pool=%+v err=%v", pool, err)
	}

	bindingResult := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "team", "subject_id": "acme/platform"}, map[string]string{"pool": "linux"}, nil)
	if bindingResult.Code != http.StatusCreated {
		t.Fatalf("create binding status=%d body=%s", bindingResult.Code, bindingResult.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || !bindings[0].Enabled {
		t.Fatalf("binding should default enabled: bindings=%+v err=%v", bindings, err)
	}

	registerResult := callSessionPoolHandler(t, controller.RegisterRunner, http.MethodPost, "/internal/session-runners/register",
		map[string]any{"runner_id": "runner-a", "pool": "linux", "pod_name": "pod-a", "capabilities": map[string]string{"dind": "true"}}, nil,
		map[string]string{"Authorization": "Bearer " + created.ConnectionToken, "X-Session-Manager-ID": "manager-a"})
	if registerResult.Code != http.StatusCreated {
		t.Fatalf("register runner status=%d body=%s", registerResult.Code, registerResult.Body.String())
	}
	var registered struct {
		RunnerToken string `json:"runner_token"`
	}
	decodeRecorder(t, registerResult, &registered)
	runner, err := store.GetRunner(context.Background(), "runner-a")
	if err != nil || runner.Capabilities["dind"] != "true" {
		t.Fatalf("runner capabilities not persisted: runner=%+v err=%v", runner, err)
	}

	if err := store.Enqueue(context.Background(), &core.Allocation{SessionID: "session-a", Pool: "linux", RuntimeToken: "runtime-secret", Requirements: map[string]string{"dind": "true"}}); err != nil {
		t.Fatal(err)
	}
	if err := routes.Save(context.Background(), &portrepos.SessionRoute{SessionID: "session-a", RemoteSessionID: "initial", Status: "starting"}); err != nil {
		t.Fatal(err)
	}
	claimResult := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s",
		nil, nil, map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if claimResult.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimResult.Code, claimResult.Body.String())
	}
	var claim struct {
		LeaseID      string `json:"lease_id"`
		RuntimeToken string `json:"runtime_token"`
	}
	decodeRecorder(t, claimResult, &claim)
	if claim.LeaseID == "" || claim.RuntimeToken != "runtime-secret" {
		t.Fatalf("invalid claim response: %+v", claim)
	}

	ackResult := callSessionPoolHandler(t, controller.AckRunnerAllocation, http.MethodPost, "/internal/session-runners/allocations/session-a/ack",
		map[string]any{"lease_id": claim.LeaseID, "remote_session_id": "adopted-stock-session"}, map[string]string{"sessionId": "session-a"},
		map[string]string{"Authorization": "Bearer " + registered.RunnerToken, "X-Session-Runner-ID": "runner-a"})
	if ackResult.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackResult.Code, ackResult.Body.String())
	}
	runner, err = store.GetRunner(context.Background(), "runner-a")
	if err != nil || runner.Status != core.RunnerRunning {
		t.Fatalf("runner should be running: runner=%+v err=%v", runner, err)
	}
	route, err := routes.Get(context.Background(), "session-a")
	if err != nil || route.RemoteSessionID != "adopted-stock-session" {
		t.Fatalf("route should use the actual local session ID: route=%+v err=%v", route, err)
	}
}

func TestRunnerCannotClaimWhileManagerHeartbeatIsStaleAndRecoversAutomatically(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	now := time.Now().UTC()
	controller.now = func() time.Time { return now }

	managerToken, managerTokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{
		ID: "manager-a", Name: "Manager A", Enabled: true,
		ConnectionTokenHash: managerTokenHash, LastHeartbeatAt: now.Add(-sessionManagerHeartbeatTTL - time.Second),
	}
	if err := store.CreateManager(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(context.Background(), &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	runnerToken, runnerTokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRunner(context.Background(), &core.Runner{ID: "runner-a", ManagerID: manager.ID, Pool: "linux", TokenHash: runnerTokenHash, Status: core.RunnerIdle}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(context.Background(), &core.Allocation{SessionID: "session-a", Pool: "linux"}); err != nil {
		t.Fatal(err)
	}

	headers := map[string]string{"Authorization": "Bearer " + runnerToken, "X-Session-Runner-ID": "runner-a"}
	stale := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s", nil, nil, headers)
	if stale.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale manager claim status=%d body=%s", stale.Code, stale.Body.String())
	}

	heartbeat := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		nil, map[string]string{"id": "manager-a"}, map[string]string{"Authorization": "Bearer " + managerToken})
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("recovery heartbeat status=%d body=%s", heartbeat.Code, heartbeat.Body.String())
	}
	recovered := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s", nil, nil, headers)
	if recovered.Code != http.StatusOK {
		t.Fatalf("recovered manager claim status=%d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestCreateClusterWidePoolBinding(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	result := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "all"}, map[string]string{"pool": "linux"}, nil)
	if result.Code != http.StatusCreated {
		t.Fatalf("create cluster-wide binding status=%d body=%s", result.Code, result.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || bindings[0].SubjectType != core.SubjectAll || bindings[0].SubjectID != "" {
		t.Fatalf("unexpected cluster-wide binding: bindings=%+v err=%v", bindings, err)
	}
	duplicate := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "all"}, map[string]string{"pool": "linux"}, nil)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate cluster-wide binding status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	invalid := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "all", "subject_id": "unexpected"}, map[string]string{"pool": "linux"}, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("cluster-wide binding with subject ID status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestBindingPriorityAndMaxConcurrentValidationAndPatch(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	if err := store.CreateLogicalPool(context.Background(), &core.LogicalPool{Name: "linux", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	invalid := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "alice", "max_concurrent": -1}, map[string]string{"pool": "linux"}, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("negative max_concurrent status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	created := callSessionPoolHandler(t, controller.CreateBinding, http.MethodPost, "/admin/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "alice", "priority": 10, "max_concurrent": 2}, map[string]string{"pool": "linux"}, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create binding status=%d body=%s", created.Code, created.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || bindings[0].Priority != 10 || bindings[0].MaxConcurrent != 2 {
		t.Fatalf("created binding quota: bindings=%+v err=%v", bindings, err)
	}

	patched := callSessionPoolHandler(t, controller.PatchBinding, http.MethodPatch, "/admin/session-pools/linux/bindings/"+bindings[0].ID,
		map[string]any{"priority": 20, "max_concurrent": 4}, map[string]string{"pool": "linux", "bindingId": bindings[0].ID}, nil)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch binding status=%d body=%s", patched.Code, patched.Body.String())
	}
	bindings, err = store.ListBindings(context.Background(), "linux")
	if err != nil || bindings[0].Priority != 20 || bindings[0].MaxConcurrent != 4 {
		t.Fatalf("patched binding quota: bindings=%+v err=%v", bindings, err)
	}
}

func TestPoolCreatorReceivesManageAndUseBinding(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAdmin, "alice")
	requireNoError(t, alice.SetRoles([]entities.Role{entities.RoleAdmin}))
	bob := entities.NewUser(entities.UserID("bob"), entities.UserTypeAPIKey, "bob")

	created := callSessionPoolHandlerAs(t, controller.CreateLogicalPool, http.MethodPost, "/session-pools",
		map[string]any{"name": "linux"}, nil, nil, alice)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", created.Code, created.Body.String())
	}
	bindings, err := store.ListBindings(context.Background(), "linux")
	if err != nil || len(bindings) != 1 || bindings[0].SubjectType != core.SubjectUser || bindings[0].SubjectID != "alice" || bindings[0].Role != core.BindingRoleManageAndUse {
		t.Fatalf("creator manage-and-use binding missing: bindings=%+v err=%v", bindings, err)
	}

	denied := callSessionPoolHandlerAs(t, controller.PatchLogicalPool, http.MethodPatch, "/session-pools/linux",
		map[string]any{"enabled": false}, map[string]string{"pool": "linux"}, nil, bob)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-manager patch status=%d body=%s", denied.Code, denied.Body.String())
	}

	granted := callSessionPoolHandlerAs(t, controller.CreateBinding, http.MethodPost, "/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "bob", "role": "manage"}, map[string]string{"pool": "linux"}, nil, alice)
	if granted.Code != http.StatusCreated {
		t.Fatalf("grant manage status=%d body=%s", granted.Code, granted.Body.String())
	}
	allowed := callSessionPoolHandlerAs(t, controller.PatchLogicalPool, http.MethodPatch, "/session-pools/linux",
		map[string]any{"enabled": false}, map[string]string{"pool": "linux"}, nil, bob)
	if allowed.Code != http.StatusOK {
		t.Fatalf("manager patch status=%d body=%s", allowed.Code, allowed.Body.String())
	}
}

func TestCreateLogicalPoolRequiresAdministrator(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	member := entities.NewUser(entities.UserID("member"), entities.UserTypeAPIKey, "member")

	result := callSessionPoolHandlerAs(t, controller.CreateLogicalPool, http.MethodPost, "/session-pools",
		map[string]any{"name": "shared"}, nil, nil, member)
	if result.Code != http.StatusForbidden {
		t.Fatalf("create pool status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestCreateBindingAcceptsMultipleRoles(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	controller := NewSessionPoolController(store, nil)
	alice := entities.NewUser(entities.UserID("alice"), entities.UserTypeAdmin, "alice")
	requireNoError(t, alice.SetRoles([]entities.Role{entities.RoleAdmin}))
	created := callSessionPoolHandlerAs(t, controller.CreateLogicalPool, http.MethodPost, "/session-pools",
		map[string]any{"name": "linux"}, nil, nil, alice)
	if created.Code != http.StatusCreated {
		t.Fatalf("create pool status=%d body=%s", created.Code, created.Body.String())
	}

	result := callSessionPoolHandlerAs(t, controller.CreateBinding, http.MethodPost, "/session-pools/linux/bindings",
		map[string]any{"subject_type": "user", "subject_id": "bob", "roles": []string{"manage", "use"}}, map[string]string{"pool": "linux"}, nil, alice)
	if result.Code != http.StatusCreated {
		t.Fatalf("create multi-role binding status=%d body=%s", result.Code, result.Body.String())
	}
	var binding core.Binding
	if err := json.Unmarshal(result.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if !binding.GrantsManage() || !binding.GrantsUse() || len(binding.Roles) != 2 {
		t.Fatalf("multi-role binding not preserved: %+v", binding)
	}
}
