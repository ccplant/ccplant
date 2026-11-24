package controllers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	"k8s.io/client-go/kubernetes/fake"
)

type statusTestTunnel struct {
	connected map[string]bool
	mu        sync.Mutex
	requests  []string
}

type runnerInventoryTunnel struct {
	statusTestTunnel
}

type staticExternalRuntimeProfile struct {
	profile *sessionsettings.RuntimeProfile
}

func (p staticExternalRuntimeProfile) ExternalRuntimeProfile() *sessionsettings.RuntimeProfile {
	copy := *p.profile
	return &copy
}

func (t *runnerInventoryTunnel) Do(_ context.Context, _, _, _ string, req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.URL.String())
	t.mu.Unlock()
	body := `{"running_runner_ids":["session-a","direct-session"],"used_runner_ids":["session-a","direct-session"]}`
	status := http.StatusOK
	if strings.Contains(req.URL.Path, "/logs") {
		body = `{"lines":["hello"],"source":"direct-session"}`
	} else if req.Method == http.MethodDelete {
		body = ""
		status = http.StatusNoContent
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
}

func (t *statusTestTunnel) IsConnected(_ context.Context, id string) bool { return t.connected[id] }
func (t *statusTestTunnel) Do(_ context.Context, id, _, _ string, req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.URL.String())
	t.mu.Unlock()
	body := `{"version":"v1.2.3","running_runners":2,"used_runners":1}`
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
}

func TestManagerRuntimeProfileAndVersionAuthentication(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	requireNoError(t, err)
	manager := &core.Manager{
		ID: "manager-a", Name: "Manager A", Scope: core.ManagerScopeUser, OwnerID: "alice",
		Enabled: true, ConnectionTokenHash: tokenHash,
	}
	requireNoError(t, store.CreateManager(context.Background(), manager))
	provider := staticExternalRuntimeProfile{profile: &sessionsettings.RuntimeProfile{Version: 1}}
	controller := NewSessionPoolController(store, nil, provider)
	profileResult := callSessionPoolHandler(t, controller.GetManagerRuntimeProfile, http.MethodGet,
		"/internal/session-managers/manager-a/runtime-profile", nil,
		map[string]string{"id": "manager-a"}, map[string]string{"Authorization": "Bearer " + token})
	if profileResult.Code != http.StatusOK {
		t.Fatalf("runtime profile status=%d body=%s", profileResult.Code, profileResult.Body.String())
	}
	var snapshot sessionallocation.RuntimeProfileSnapshot
	decodeRecorder(t, profileResult, &snapshot)
	if snapshot.Revision == "" {
		t.Fatalf("runtime profile=%+v", snapshot)
	}
	versionResult := callSessionPoolHandler(t, controller.GetManagerVersion, http.MethodGet,
		"/internal/session-managers/manager-a/version", nil,
		map[string]string{"id": "manager-a"}, map[string]string{"Authorization": "Bearer " + token})
	if versionResult.Code != http.StatusOK || !strings.Contains(versionResult.Body.String(), `"version":`) {
		t.Fatalf("version status=%d body=%s", versionResult.Code, versionResult.Body.String())
	}
	unauthorizedVersion := callSessionPoolHandler(t, controller.GetManagerVersion, http.MethodGet,
		"/internal/session-managers/manager-a/version", nil,
		map[string]string{"id": "manager-a"}, map[string]string{"Authorization": "Bearer invalid"})
	if unauthorizedVersion.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized version status=%d body=%s", unauthorizedVersion.Code, unauthorizedVersion.Body.String())
	}

}

func TestSaveManagerManagedFilesPersistsInParentStore(t *testing.T) {
	client := fake.NewSimpleClientset()
	store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
	token, tokenHash, err := newSessionRunnerToken()
	requireNoError(t, err)
	requireNoError(t, store.CreateManager(context.Background(), &core.Manager{ID: "manager-a", Enabled: true, ConnectionTokenHash: tokenHash}))
	routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
	requireNoError(t, routes.Save(context.Background(), &portrepos.SessionRoute{SessionID: "session-a", RemoteSessionID: "runtime-a", ManagerID: "manager-a", UserID: "alice"}))
	files := &managedFilesStoreStub{}
	controller := NewSessionPoolController(store, routes).WithManagedFileStore(files)

	result := callSessionPoolHandler(t, controller.SaveManagerManagedFiles, http.MethodPut,
		"/internal/session-managers/manager-a/sessions/runtime-a/managed-files",
		map[string]any{"owner": "alice", "files": []map[string]any{{"path": "/home/agentapi/.codex/auth.json", "content": "updated"}}},
		map[string]string{"id": "manager-a", "sessionId": "runtime-a"},
		map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusNoContent {
		t.Fatalf("managed files status=%d body=%s", result.Code, result.Body.String())
	}
	if files.owner != "alice" || len(files.files) != 1 {
		t.Fatalf("persisted owner=%q files=%d", files.owner, len(files.files))
	}
}

func TestManagerAndRunnerLogsUseSeparateEndpoints(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	requestCtx := context.Background()
	requireNoError(t, store.CreateLogicalPool(requestCtx, &core.LogicalPool{Name: "managed", Enabled: true}))
	requireNoError(t, store.CreateManager(requestCtx, &core.Manager{ID: "manager-a", Enabled: true}))
	requireNoError(t, store.CreatePoolSupplier(requestCtx, &core.PoolSupplier{Pool: "managed", ManagerID: "manager-a", Enabled: true}))
	requireNoError(t, store.CreateBinding(requestCtx, &core.Binding{Pool: "managed", SubjectType: core.SubjectUser, SubjectID: "alice", Role: core.BindingRoleManage, Enabled: true}))
	requireNoError(t, store.CreateRunner(requestCtx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "managed", Status: core.RunnerRunning}))
	requireNoError(t, store.Enqueue(requestCtx, &core.Allocation{SessionID: "session-a", Pool: "managed", ManagerID: "manager-a", RunnerID: "runner-a", Status: core.AllocationRunning}))
	tunnel := &statusTestTunnel{connected: map[string]bool{"manager-a": true}}
	controller := NewSessionPoolController(store, nil).WithManagerTunnel(tunnel)
	user := entities.NewUser("alice", entities.UserTypeRegular, "alice")
	requireNoError(t, user.SetRoles([]entities.Role{entities.RoleAdmin}))

	managerLogs := callSessionPoolHandlerAs(t, controller.GetManagerLogs, http.MethodGet, "/session-managers/manager-a/logs?tail=10", nil, map[string]string{"id": "manager-a"}, nil, user)
	if managerLogs.Code != http.StatusOK {
		t.Fatalf("manager logs status=%d body=%s", managerLogs.Code, managerLogs.Body.String())
	}
	runnerLogs := callSessionPoolHandlerAs(t, controller.GetRunnerLogs, http.MethodGet, "/session-runners/runner-a/logs?tail=20", nil, map[string]string{"id": "runner-a"}, nil, user)
	if runnerLogs.Code != http.StatusOK {
		t.Fatalf("runner logs status=%d body=%s", runnerLogs.Code, runnerLogs.Body.String())
	}

	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if len(tunnel.requests) != 2 || tunnel.requests[0] != "http://manager/internal/esm-management/logs?tail=10" || tunnel.requests[1] != "http://manager/internal/esm-management/logs?runner_id=runner-a&session_id=session-a&tail=20" {
		t.Fatalf("requests=%v", tunnel.requests)
	}
}

func TestAdminManagerAndSessionLogs(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	requestCtx := context.Background()
	requireNoError(t, store.CreateManager(requestCtx, &core.Manager{ID: "manager-a", Enabled: true}))
	tunnel := &statusTestTunnel{connected: map[string]bool{"manager-a": true}}
	controller := NewSessionPoolController(store, nil).WithManagerTunnel(tunnel)

	managerLogs := callSessionPoolHandler(t, controller.GetAdminManagerLogs, http.MethodGet, "/admin/session-managers/manager-a/logs?tail=11", nil, map[string]string{"id": "manager-a"}, nil)
	if managerLogs.Code != http.StatusOK {
		t.Fatalf("manager logs status=%d body=%s", managerLogs.Code, managerLogs.Body.String())
	}
	sessionLogs := callSessionPoolHandler(t, controller.GetAdminRunnerLogs, http.MethodGet, "/admin/session-runners/runner-a/logs?manager_id=manager-a&session_id=session-a&tail=12", nil, map[string]string{"id": "runner-a"}, nil)
	if sessionLogs.Code != http.StatusOK {
		t.Fatalf("session logs status=%d body=%s", sessionLogs.Code, sessionLogs.Body.String())
	}

	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if len(tunnel.requests) != 2 || tunnel.requests[0] != "http://manager/internal/esm-management/logs?tail=11" || tunnel.requests[1] != "http://manager/internal/esm-management/logs?runner_id=runner-a&session_id=session-a&tail=12" {
		t.Fatalf("requests=%v", tunnel.requests)
	}
}

func TestListManageablePoolStatusFiltersPoolsAndFetchesLiveManagerStatus(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	for _, pool := range []string{"managed", "use-only"} {
		requireNoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: pool, Enabled: true}))
	}
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "A", Enabled: true}))
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-b", Name: "B", Enabled: true}))
	requireNoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "managed", ManagerID: "manager-a", Enabled: true}))
	requireNoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "use-only", ManagerID: "manager-b", Enabled: true}))
	requireNoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "managed", SubjectType: core.SubjectUser, SubjectID: "alice", Role: core.BindingRoleManage, Enabled: true}))
	requireNoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "use-only", SubjectType: core.SubjectUser, SubjectID: "alice", Role: core.BindingRoleUse, Enabled: true}))
	controller := NewSessionPoolController(store, nil).WithManagerTunnel(&statusTestTunnel{connected: map[string]bool{"manager-a": true}})
	user := entities.NewUser("alice", entities.UserTypeRegular, "alice")
	rec := callSessionPoolHandlerAs(t, controller.ListManageablePoolStatus, http.MethodGet, "/session-pools/status", nil, nil, nil, user)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Pools    []core.LogicalPool `json:"session_pools"`
		Managers []struct {
			Manager core.Manager           `json:"manager"`
			Online  bool                   `json:"online"`
			Status  map[string]interface{} `json:"status"`
		} `json:"session_managers"`
	}
	decodeRecorder(t, rec, &result)
	if len(result.Pools) != 1 || result.Pools[0].Name != "managed" {
		t.Fatalf("pools=%+v", result.Pools)
	}
	if len(result.Managers) != 1 || result.Managers[0].Manager.ID != "manager-a" || !result.Managers[0].Online || result.Managers[0].Status["version"] != "v1.2.3" {
		t.Fatalf("managers=%+v", result.Managers)
	}
}

func TestAdminRunnerInventoryIncludesPooledAndDirectRunners(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerRunning}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "retired-runner", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerDraining}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: "manager-a", RunnerID: "runner-a"}))
	tunnel := &runnerInventoryTunnel{statusTestTunnel: statusTestTunnel{connected: map[string]bool{"manager-a": true}}}
	controller := NewSessionPoolController(store, nil).WithManagerTunnel(tunnel).WithManagerLiveness(&testManagerLiveness{
		connected: map[string]bool{"manager-a": true, "session-a": true, "direct-session": true},
	})

	rec := callSessionPoolHandler(t, controller.ListAdminRunners, http.MethodGet, "/admin/session-runners", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Runners []adminRunnerInventoryItem `json:"session_runners"`
	}
	decodeRecorder(t, rec, &result)
	if len(result.Runners) != 2 {
		t.Fatalf("runners=%+v", result.Runners)
	}
	byID := map[string]adminRunnerInventoryItem{}
	for _, runner := range result.Runners {
		byID[runner.ID] = runner
	}
	if pooled := byID["runner-a"]; !pooled.FromPool || pooled.Pool != "linux" || pooled.SessionID != "session-a" || !pooled.Online || pooled.Connectivity.Source != "direct_runtime" || pooled.WorkloadState != workloadPresent || !pooled.ManagerOnline || pooled.ManagerConnectivity != runnerConnectivityOnline {
		t.Fatalf("pooled=%+v", pooled)
	}
	if direct := byID["direct-session"]; direct.FromPool || direct.SessionID != "direct-session" || direct.Status != core.RunnerRunning || !direct.Online {
		t.Fatalf("direct=%+v", direct)
	}

	logs := callSessionPoolHandler(t, controller.GetAdminRunnerLogs, http.MethodGet, "/admin/session-runners/direct-session/logs?manager_id=manager-a", nil, map[string]string{"id": "direct-session"}, nil)
	if logs.Code != http.StatusOK || !strings.Contains(logs.Body.String(), "hello") {
		t.Fatalf("logs status=%d body=%s", logs.Code, logs.Body.String())
	}
}

func TestAdminRunnerInventoryUsesDirectRuntimeWhenManagerIsUnconfirmed(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerRunning}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: "manager-a", RunnerID: "runner-a"}))
	controller := NewSessionPoolController(store, nil).WithManagerTunnel(&runnerInventoryTunnel{
		statusTestTunnel: statusTestTunnel{connected: map[string]bool{"manager-a": false}},
	}).WithManagerLiveness(&testManagerLiveness{connected: map[string]bool{"session-a": true}})

	rec := callSessionPoolHandler(t, controller.ListAdminRunners, http.MethodGet, "/admin/session-runners", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Runners []adminRunnerInventoryItem `json:"session_runners"`
	}
	decodeRecorder(t, rec, &result)
	if len(result.Runners) != 1 {
		t.Fatalf("runners=%+v", result.Runners)
	}
	runner := result.Runners[0]
	if !runner.Online || runner.Status != core.RunnerRunning || runner.Connectivity.State != runnerConnectivityOnline || runner.Connectivity.Source != "direct_runtime" || runner.ManagerOnline || runner.WorkloadState != workloadUnknown {
		t.Fatalf("runner=%+v", runner)
	}
}

func TestAdminRunnerInventoryUsesRunnerAPILivenessForIdleRunner(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	now := time.Now().UTC()
	requireNoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux", Status: core.RunnerIdle}))
	controller := NewSessionPoolController(store, nil)
	controller.now = func() time.Time { return now }

	rec := callSessionPoolHandler(t, controller.ListAdminRunners, http.MethodGet, "/admin/session-runners", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Runners []adminRunnerInventoryItem `json:"session_runners"`
	}
	decodeRecorder(t, rec, &result)
	if len(result.Runners) != 1 || !result.Runners[0].Online || result.Runners[0].Connectivity.Source != "runner_api" || result.Runners[0].ManagerOnline {
		t.Fatalf("runners=%+v", result.Runners)
	}
}

func TestAdminRunnerInventorySystemScopeExcludesUserAndTeamManagers(t *testing.T) {
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	for _, manager := range []*core.Manager{
		{ID: "system-manager", Scope: core.ManagerScopeSystem, Enabled: true},
		{ID: "user-manager", Scope: core.ManagerScopeUser, OwnerID: "alice", Enabled: true},
		{ID: "team-manager", Scope: core.ManagerScopeTeam, OwnerID: "team-a", Enabled: true},
	} {
		requireNoError(t, store.CreateManager(ctx, manager))
		requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: manager.ID + "-runner", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerIdle}))
	}
	controller := NewSessionPoolController(store, nil)
	rec := callSessionPoolHandler(t, controller.ListAdminRunners, http.MethodGet, "/admin/session-runners?scope=system", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Runners []adminRunnerInventoryItem `json:"session_runners"`
	}
	decodeRecorder(t, rec, &result)
	if len(result.Runners) != 1 || result.Runners[0].ManagerID != "system-manager" {
		t.Fatalf("runners=%+v", result.Runners)
	}

	logs := callSessionPoolHandler(t, controller.GetAdminManagerLogs, http.MethodGet, "/admin/session-managers/user-manager/logs", nil, map[string]string{"id": "user-manager"}, nil)
	if logs.Code != http.StatusNotFound {
		t.Fatalf("user manager logs status=%d body=%s", logs.Code, logs.Body.String())
	}
}

func TestSessionManagerHeartbeatCollectsExpiredDrainingRunners(t *testing.T) {
	ctx := context.Background()
	store := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		t.Fatal(err)
	}
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	requireNoError(t, store.CreateManager(ctx, manager))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "expired", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerDraining}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "local", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerDraining}))
	requireNoError(t, store.CreateRunner(ctx, &core.Runner{ID: "allocated", ManagerID: manager.ID, Pool: "linux", Status: core.RunnerDraining}))
	requireNoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", ManagerID: manager.ID, RunnerID: "allocated"}))

	controller := NewSessionPoolController(store, nil)
	controller.now = func() time.Time { return time.Now().UTC().Add(sessionRunnerDrainingRetention + time.Minute) }
	localIDs := []string{"local"}
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/manager-a/heartbeat",
		map[string]any{"local_runner_ids": localIDs}, map[string]string{"id": manager.ID}, map[string]string{"Authorization": "Bearer " + token})
	if result.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", result.Code, result.Body.String())
	}
	if _, err := store.GetRunner(ctx, "expired"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expired draining runner was not collected: %v", err)
	}
	for _, id := range []string{"local", "allocated"} {
		if _, err := store.GetRunner(ctx, id); err != nil {
			t.Fatalf("protected draining runner %s was removed: %v", id, err)
		}
	}
}
