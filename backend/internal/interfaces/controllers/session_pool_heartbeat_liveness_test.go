package controllers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
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

type failingTouchLiveness struct {
	testManagerLiveness
	err error
}

func (l *failingTouchLiveness) TouchManager(context.Context, string, string) error {
	return l.err
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

func TestSessionManagerHeartbeatMarksManagerAliveOnlyWhenItSucceeds(t *testing.T) {
	store, manager, token := newHeartbeatLivenessFixture(t)
	liveness := &testManagerLiveness{connected: map[string]bool{}}
	controller := NewSessionPoolController(store, nil).WithManagerLiveness(liveness)

	store.fail = true
	require.NotEqual(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	require.False(t, liveness.connected[manager.ID], "a failed heartbeat must not mark the manager alive")

	store.fail = false
	require.Equal(t, http.StatusOK, callHeartbeat(t, controller, manager.ID, token))
	require.True(t, liveness.connected[manager.ID])
}

func TestSessionManagerHeartbeatFailsWhenAliveMarkCannotBeWritten(t *testing.T) {
	store, manager, token := newHeartbeatLivenessFixture(t)
	liveness := &failingTouchLiveness{testManagerLiveness: testManagerLiveness{connected: map[string]bool{}}, err: errors.New("redis unavailable")}
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

func TestManagerControlPollingDoesNotMarkManagerAlive(t *testing.T) {
	sessionRunners, manager, token := newHeartbeatLivenessFixture(t)
	control := &runtimeControllerStore{}
	controller := NewESMControlController(control, nil, sessionRunners)

	for _, call := range []struct {
		method  string
		path    string
		body    string
		handler echo.HandlerFunc
	}{
		{http.MethodGet, "/internal/external-session-managers/manager-a/control/commands", "", controller.WaitCommands},
		{http.MethodPost, "/internal/external-session-managers/manager-a/control/frames", `{"frames":[{"id":"frame-a","request_id":"request-a","sequence":1}]}`, controller.AppendFrames},
	} {
		req := httptest.NewRequest(call.method, call.path, bytes.NewBufferString(call.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ctx := echo.New().NewContext(req, rec)
		ctx.SetParamNames("managerId")
		ctx.SetParamValues(manager.ID)
		require.NoError(t, call.handler(ctx))
		require.Less(t, rec.Code, 300, "%s %s: %s", call.method, call.path, rec.Body.String())
		require.Empty(t, control.touched, "%s must not mark the manager alive; only a completed heartbeat does", call.path)
	}
}
