package app

import (
	"context"
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
	managerID, err := server.legacyAllocatorManagerForPool(ctx, "pool-a")
	require.NoError(t, err)
	require.Equal(t, "manager-a", managerID)
}
