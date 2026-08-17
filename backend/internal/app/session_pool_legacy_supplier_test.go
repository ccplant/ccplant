package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	sessionrunnerinfra "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLegacyAllocatorManagerForPool(t *testing.T) {
	store := sessionrunnerinfra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	require.NoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Enabled: true,
		Capabilities: []string{core.CapabilityLegacyAllocatorV1}}))
	require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "pool-a", Enabled: true}))
	require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "pool-a", ManagerID: "manager-a", Enabled: true}))

	server := &Server{sessionRunnerStore: store}
	managerID, err := server.legacyAllocatorManagerForPool(ctx, "pool-a", "session-1")
	require.NoError(t, err)
	require.Equal(t, "manager-a", managerID)
}

func TestLegacyAllocatorManagerForPoolDistributesAcrossEqualSuppliers(t *testing.T) {
	store := sessionrunnerinfra.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	ctx := context.Background()
	require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "pool-a", Enabled: true}))
	for _, managerID := range []string{"manager-a", "manager-b"} {
		require.NoError(t, store.CreateManager(ctx, &core.Manager{ID: managerID, Enabled: true,
			Capabilities: []string{core.CapabilityLegacyAllocatorV1}}))
		require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "pool-a", ManagerID: managerID, Enabled: true}))
	}

	server := &Server{sessionRunnerStore: store}
	selected := map[string]bool{}
	for i := 0; i < 100; i++ {
		managerID, err := server.legacyAllocatorManagerForPool(ctx, "pool-a", fmt.Sprintf("session-%d", i))
		require.NoError(t, err)
		selected[managerID] = true
	}
	require.Equal(t, map[string]bool{"manager-a": true, "manager-b": true}, selected)
}
