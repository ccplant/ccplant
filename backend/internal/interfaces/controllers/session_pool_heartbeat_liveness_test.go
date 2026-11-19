package controllers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

type failingPoolSupplierStore struct {
	*infra.Store
	fail                bool
	duringPoolSuppliers func()
}

func (s *failingPoolSupplierStore) ListPoolSuppliers(ctx context.Context) ([]*core.PoolSupplier, error) {
	if s.fail {
		return nil, errors.New("pool suppliers unavailable")
	}
	if s.duringPoolSuppliers != nil {
		s.duringPoolSuppliers()
	}
	return s.Store.ListPoolSuppliers(ctx)
}

func newHeartbeatLivenessFixture(t *testing.T) (*failingPoolSupplierStore, *core.Manager, string) {
	t.Helper()
	ctx := context.Background()
	store := &failingPoolSupplierStore{Store: infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")}
	token, tokenHash, err := newSessionRunnerToken()
	require.NoError(t, err)
	manager := &core.Manager{ID: "manager-a", Name: "Manager A", Enabled: true, ConnectionTokenHash: tokenHash}
	require.NoError(t, store.CreateManager(ctx, manager))
	require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}))
	require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: manager.ID, Enabled: true}))
	return store, manager, token
}

func callHeartbeat(t *testing.T, controller *SessionPoolController, managerID, token string) int {
	t.Helper()
	result := callSessionPoolHandler(t, controller.HeartbeatManager, http.MethodPost, "/internal/session-managers/"+managerID+"/heartbeat",
		map[string]any{"local_runner_ids": []string{}}, map[string]string{"id": managerID}, map[string]string{"Authorization": "Bearer " + token})
	return result.Code
}

func TestSessionManagerHeartbeatRecordsCompletionOnlyWhenItSucceeds(t *testing.T) {
	store, manager, token := newHeartbeatLivenessFixture(t)
	liveness := &testManagerLiveness{connected: map[string]bool{}}
	controller := NewSessionPoolController(store, nil).WithManagerLiveness(liveness)

	store.fail = true
	require.NotEqual(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	require.True(t, liveness.connected[manager.ID], "a failed heartbeat still proves the manager can reach the API")
	require.Zero(t, liveness.heartbeats[manager.ID], "a failed heartbeat must not mark the manager as schedulable")

	store.fail = false
	require.Equal(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	require.Equal(t, 1, liveness.heartbeats[manager.ID])
}

func TestSessionManagerHeartbeatFailsWhenCompletionCannotBeRecorded(t *testing.T) {
	store, manager, token := newHeartbeatLivenessFixture(t)
	liveness := &testManagerLiveness{connected: map[string]bool{}, recordError: errors.New("redis unavailable")}
	controller := NewSessionPoolController(store, nil).WithManagerLiveness(liveness)

	require.Equal(t, http.StatusServiceUnavailable, callHeartbeat(t, controller, manager.ID, token))
}

func TestSessionManagerHeartbeatWithoutSharedLivenessPersistsOnlyCompletedHeartbeats(t *testing.T) {
	store, manager, token := newHeartbeatLivenessFixture(t)
	now := time.Now().UTC()
	controller := NewSessionPoolController(store, nil)
	controller.now = func() time.Time { return now }

	store.fail = true
	require.NotEqual(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	persisted, err := store.GetManager(context.Background(), manager.ID)
	require.NoError(t, err)
	require.True(t, persisted.LastHeartbeatAt.IsZero(), "a failed heartbeat must not refresh the persisted heartbeat time")

	store.fail = false
	require.Equal(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	persisted, err = store.GetManager(context.Background(), manager.ID)
	require.NoError(t, err)
	require.WithinDuration(t, now, persisted.LastHeartbeatAt, time.Second)
}

func TestSessionManagerHeartbeatDoesNotRevertConcurrentManagerChanges(t *testing.T) {
	ctx := context.Background()
	store, manager, token := newHeartbeatLivenessFixture(t)
	controller := NewSessionPoolController(store, nil)
	_, rotatedHash, err := newSessionRunnerToken()
	require.NoError(t, err)
	store.duringPoolSuppliers = func() {
		store.duringPoolSuppliers = nil
		current, getErr := store.GetManager(ctx, manager.ID)
		require.NoError(t, getErr)
		current.Enabled = false
		current.ConnectionTokenHash = rotatedHash
		require.NoError(t, store.UpdateManager(ctx, current))
	}

	require.Equal(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))

	persisted, err := store.GetManager(ctx, manager.ID)
	require.NoError(t, err)
	require.False(t, persisted.Enabled, "a heartbeat must not re-enable a manager disabled while it was running")
	require.Equal(t, rotatedHash, persisted.ConnectionTokenHash, "a heartbeat must not restore a revoked connection token")
	require.False(t, persisted.LastHeartbeatAt.IsZero())
}

func TestRunnerClaimRequiresSchedulableManagerNotOnlyControlConnection(t *testing.T) {
	ctx := context.Background()
	store, manager, _ := newHeartbeatLivenessFixture(t)
	runnerToken, runnerTokenHash, err := newSessionRunnerToken()
	require.NoError(t, err)
	require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: manager.ID, Pool: "linux", TokenHash: runnerTokenHash, Status: core.RunnerIdle}))
	require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux"}))
	liveness := &testManagerLiveness{connected: map[string]bool{manager.ID: true}, unhealthy: map[string]bool{manager.ID: true}}
	controller := NewSessionPoolController(store, nil).WithManagerLiveness(liveness)
	headers := map[string]string{"Authorization": "Bearer " + runnerToken, "X-Session-Runner-ID": "runner-a"}

	stale := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s", nil, nil, headers)
	require.Equal(t, http.StatusServiceUnavailable, stale.Code, stale.Body.String())

	liveness.unhealthy[manager.ID] = false
	claimed := callSessionPoolHandler(t, controller.ClaimRunnerAllocation, http.MethodGet, "/internal/session-runners/allocations/next?wait=0s", nil, nil, headers)
	require.Equal(t, http.StatusOK, claimed.Code, claimed.Body.String())
}

func TestAvailablePoolsExcludeManagersWithStaleHeartbeat(t *testing.T) {
	ctx := context.Background()
	store, manager, _ := newHeartbeatLivenessFixture(t)
	require.NoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "alice", Enabled: true}))
	liveness := &testManagerLiveness{connected: map[string]bool{manager.ID: true}, unhealthy: map[string]bool{manager.ID: true}}
	resolver := core.NewResolver(store, 90*time.Second).WithManagerLiveness(liveness)
	alice := core.Subject{Type: core.SubjectUser, ID: "alice"}

	resolved, err := resolver.Resolve(ctx, alice, "", nil)
	require.NoError(t, err)
	require.Nil(t, resolved, "a manager whose heartbeat is stale must not receive new sessions")

	liveness.unhealthy[manager.ID] = false
	resolved, err = resolver.Resolve(ctx, alice, "", nil)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "linux", resolved.Pool.Name)
}
