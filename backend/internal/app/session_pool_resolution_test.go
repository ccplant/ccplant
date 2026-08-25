package app

import (
	"context"
	"testing"
	"time"

	coreesm "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

type connectedManagerStore struct {
	coreesm.Store
	connected bool
}

func (s connectedManagerStore) IsManagerConnected(context.Context, string) (bool, error) {
	return s.connected, nil
}

func TestResolveSessionPoolUsesControlStoreLiveness(t *testing.T) {
	ctx := context.Background()
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	manager := &sessionrunnercore.Manager{
		ID: "manager-a", Enabled: true,
		// Deliberately stale: Redis liveness must override persisted heartbeat.
		LastHeartbeatAt: time.Now().Add(-time.Hour),
	}
	if err := store.CreateManager(ctx, manager); err != nil {
		t.Fatal(err)
	}
	pool := &sessionrunnercore.LogicalPool{Name: "managed", Enabled: true}
	if err := store.CreateLogicalPool(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePoolSupplier(ctx, &sessionrunnercore.PoolSupplier{ManagerID: manager.ID, Pool: pool.Name, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBinding(ctx, &sessionrunnercore.Binding{Pool: pool.Name, SubjectType: sessionrunnercore.SubjectUser, SubjectID: "alice", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	server := &Server{sessionRunnerStore: store, esmControlStore: connectedManagerStore{connected: true}}
	resolved, err := server.resolveSessionPool(ctx, sessionrunnercore.Subject{Type: sessionrunnercore.SubjectUser, ID: "alice"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.Pool.Name != "managed" {
		t.Fatalf("resolved pool = %#v, want managed", resolved)
	}
}
