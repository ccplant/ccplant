package controllers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMissingRunnerRequeuesUnstartedButPreservesStarted(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstarted", true: "started"}[started], func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset()
			s := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
			routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
			require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "old", ManagerID: "manager", Pool: "pool"}))
			require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool", RuntimeToken: "token", ProvisionSettings: []byte(`{"initial_message":"hello"}`)}))
			a, ok, err := s.ClaimNext(ctx, "pool", "old", time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			_, err = s.Acknowledge(ctx, a.SessionID, "old", a.LeaseID)
			require.NoError(t, err)
			if started {
				require.NoError(t, s.MarkStarted(ctx, a.SessionID, a.Generation))
			}
			require.NoError(t, routes.Save(ctx, &ports.SessionRoute{SessionID: a.SessionID, ManagerID: "manager", RemoteSessionID: "old", Generation: a.Generation, UserID: "alice", InitialMessage: "hello"}))
			c := NewSessionPoolController(s, routes)
			require.NoError(t, s.DeleteRunner(ctx, "old"))
			require.NoError(t, c.reconcileMissingManagerRunners(ctx, "manager", []string{}))
			recovered, err := s.GetAllocation(ctx, a.SessionID)
			require.NoError(t, err)
			if started {
				require.Equal(t, core.AllocationRunning, recovered.Status)
				require.Equal(t, a.Generation, recovered.Generation)
				return
			}
			require.Equal(t, core.AllocationPending, recovered.Status)
			require.Equal(t, a.Generation+1, recovered.Generation)
			require.Equal(t, a.ProvisionSettings, recovered.ProvisionSettings)
			require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "new", ManagerID: "manager", Pool: "pool"}))
			next, ok, err := s.ClaimNext(ctx, "pool", "new", time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			runner, err := s.GetRunner(ctx, "new")
			require.NoError(t, err)
			require.NoError(t, c.prepareClaimRoute(ctx, next, runner))
			route, err := routes.Get(ctx, a.SessionID)
			require.NoError(t, err)
			require.Equal(t, "new", route.RemoteSessionID)
			require.Equal(t, next.Generation, route.Generation)
			require.Equal(t, next.RuntimeTokenHash, route.RuntimeTokenHash)
			require.Equal(t, "alice", route.UserID)
			require.Equal(t, "hello", route.InitialMessage)
		})
	}
}
func TestRetireEndpointRefusesClaimingRunner(t *testing.T) {
	ctx := context.Background()
	s := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	token, hash, err := newSessionRunnerToken()
	require.NoError(t, err)
	require.NoError(t, s.CreateManager(ctx, &core.Manager{ID: "manager", ConnectionTokenHash: hash}))
	require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool"}))
	_, ok, err := s.ClaimNext(ctx, "pool", "runner", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	c := NewSessionPoolController(s, nil)
	params := map[string]string{"id": "manager", "runnerId": "runner"}
	res := callSessionPoolHandler(t, c.RetireRunner, http.MethodPost, "/internal/session-managers/manager/runners/runner/retire", nil, params, map[string]string{"Authorization": "Bearer " + token})
	require.Equal(t, http.StatusConflict, res.Code)
	res = callSessionPoolHandler(t, c.RetireRunner, http.MethodPost, "/internal/session-managers/manager/runners/runner/retire", nil, params, map[string]string{"Authorization": "Bearer invalid"})
	require.Equal(t, http.StatusUnauthorized, res.Code)
}

func TestRuntimeFencingChecksAllocationEvenWithStaleRoute(t *testing.T) {
	ctx := context.Background()
	s := infra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	require.NoError(t, s.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool"}))
	require.NoError(t, s.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "pool", RuntimeToken: "secret"}))
	a, ok, err := s.ClaimNext(ctx, "pool", "runner", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	route := &ports.SessionRoute{SessionID: "session-a", Transport: ports.SessionRouteTransportDirectRuntime, RemoteSessionID: "runner", Generation: a.Generation, RuntimeTokenHash: runtimeTokenHash("secret")}
	_, err = s.RequeueUnstarted(ctx, a.SessionID, "runner")
	require.NoError(t, err)
	runtime := &runtimeControllerStore{}
	controller := NewSessionRuntimeController(runtime, &runtimeRouteRepo{route: route}).WithRunnerStore(s)
	request, rec := runtimeControllerContext(http.MethodGet, "/internal/session-runtime/session-a/requests?generation=1", "secret")
	require.NoError(t, controller.WaitRequests(request))
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Empty(t, runtime.touched)
}
