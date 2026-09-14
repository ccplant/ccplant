package controllers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	infra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHeartbeatPreservesClaimBeforeAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  core.RunnerStatus
		expired bool
	}{
		{"claiming", core.RunnerClaiming, false}, {"lease-before-runner-update", core.RunnerIdle, false}, {"lost-runner-recovery", core.RunnerClaiming, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset()
			store := infra.NewStore(kvstore.NewKubernetesStore(client), "test")
			routes := repositories.NewKubernetesSessionRouteRepository(client, "test")
			token, hash, err := newSessionRunnerToken()
			if err != nil {
				t.Fatal(err)
			}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(store.CreateManager(ctx, &core.Manager{ID: "manager", Enabled: true, ConnectionTokenHash: hash}))
			must(store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "pool", Enabled: true}))
			must(store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "pool", ManagerID: "manager", Enabled: true}))
			must(store.CreateRunner(ctx, &core.Runner{ID: "runner", ManagerID: "manager", Pool: "pool", Status: tc.status, LastSeen: time.Now()}))
			must(store.Enqueue(ctx, &core.Allocation{SessionID: "session", Pool: "pool"}))
			lease := 45 * time.Second
			if tc.expired {
				lease = -time.Second
			}
			a, found, err := store.ClaimNext(ctx, "pool", "runner", lease)
			must(err)
			if !found {
				t.Fatal("not claimed")
			}
			must(routes.Save(ctx, &portrepos.SessionRoute{SessionID: "session", RemoteSessionID: "runner", ManagerID: "manager", Status: "starting"}))
			c := NewSessionPoolController(store, routes)
			heartbeat := func(body any) ([]string, []string) {
				r := callSessionPoolHandler(t, c.HeartbeatManager, http.MethodPost, "/heartbeat", body, map[string]string{"id": "manager"}, map[string]string{"Authorization": "Bearer " + token})
				if r.Code != http.StatusOK {
					t.Fatalf("heartbeat %d: %s", r.Code, r.Body.String())
				}
				var d struct {
					RegisteredRunnerIDs []string             `json:"registered_runner_ids"`
					AllocatedRunnerIDs  []string             `json:"allocated_runner_ids"`
					Pools               []*core.PoolSupplier `json:"pools"`
				}
				decodeRecorder(t, r, &d)
				if body != nil && (len(d.Pools) != 1 || d.Pools[0].TotalRunners != 0 || d.Pools[0].IdleRunners != 0) {
					t.Fatalf("missing runner still consumes capacity: %+v", d.Pools)
				}
				return d.RegisteredRunnerIDs, d.AllocatedRunnerIDs
			}
			registered, allocated := heartbeat(nil)
			if len(registered) != 1 || len(allocated) != 1 {
				t.Fatalf("unexpected heartbeat: %v %v", registered, allocated)
			}

			if !tc.expired && !time.Now().Before(a.LeaseExpiresAt) {
				t.Fatal("lease already expired")
			}
			heartbeat(map[string]any{"local_runner_ids": []string{}})
			if _, err := store.GetRunner(ctx, "runner"); err != nil {
				t.Fatalf("runner was removed: %v", err)
			}
			if _, err := store.GetAllocation(ctx, "session"); err != nil {
				t.Fatalf("allocation was removed: %v", err)
			}
			r, err := routes.Get(ctx, "session")
			must(err)
			if r == nil || r.Status != "starting" || r.RemoteSessionID != "runner" {
				t.Fatalf("route changed: %+v", r)
			}
			if tc.expired {
				r.Status = "stopped"
				r.StatusUpdatedAt = time.Now().Add(-time.Hour)
				must(routes.Save(ctx, r))
				must(store.CreateRunner(ctx, &core.Runner{ID: "replacement", ManagerID: "manager", Pool: "pool", Status: core.RunnerIdle}))
				next, found, err := store.ClaimNext(ctx, "pool", "replacement", 45*time.Second)
				must(err)
				if !found {
					t.Fatal("lost allocation cannot be recovered")
				}
				if next.Generation <= a.Generation {
					t.Fatal("replacement did not fence old runtime generation")
				}
				must(c.prepareClaimRoute(ctx, next, &core.Runner{ID: "replacement", ManagerID: "manager"}))
				recovered, err := routes.Get(ctx, "session")
				must(err)
				if recovered.RemoteSessionID != "replacement" {
					t.Fatal("route not repaired")
				}
				if recovered.Status != "starting" || !recovered.StatusUpdatedAt.After(r.StatusUpdatedAt) {
					t.Fatal("replacement inherited old completion TTL")
				}
				_, err = store.Acknowledge(ctx, a.SessionID, a.RunnerID, a.LeaseID)
				if !errors.Is(err, core.ErrConflict) {
					t.Fatal("old lease can still acknowledge")
				}
				a = next
			}
			_, err = store.Acknowledge(ctx, a.SessionID, a.RunnerID, a.LeaseID)
			must(err)
		})
	}
}
