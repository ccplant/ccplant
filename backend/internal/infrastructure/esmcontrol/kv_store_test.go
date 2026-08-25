package esmcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKVStoreControlLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewKVStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")

	require.NoError(t, store.TouchManager(ctx, "manager-a", "instance-a"))
	connected, err := store.IsManagerConnected(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, connected)

	command := core.Command{ID: "request-a", ManagerID: "manager-a", SessionID: "session-a", Method: "GET", Path: "/status"}
	streamID, err := store.EnqueueCommand(ctx, "manager-a", command)
	require.NoError(t, err)
	require.NotEmpty(t, streamID)

	commands, err := store.ReadCommands(ctx, "manager-a", "", 0, 10)
	require.NoError(t, err)
	require.Len(t, commands, 1)
	require.Equal(t, streamID, commands[0].StreamID)
	require.NoError(t, store.AckCommand(ctx, "manager-a", streamID))
	commands, err = store.ReadCommands(ctx, "manager-a", "", 0, 10)
	require.NoError(t, err)
	require.Empty(t, commands)

	owned, err := store.RequestBelongsToManager(ctx, "request-a", "manager-a")
	require.NoError(t, err)
	require.True(t, owned)

	frame := core.ResponseFrame{ID: "frame-a", RequestID: "request-a", Status: 200, Done: true}
	frameID, err := store.AppendFrames(ctx, "request-a", []core.ResponseFrame{frame})
	require.NoError(t, err)
	require.NotEmpty(t, frameID)
	// Retries are idempotent by request/frame ID.
	duplicateID, err := store.AppendFrames(ctx, "request-a", []core.ResponseFrame{frame})
	require.NoError(t, err)
	require.Empty(t, duplicateID)

	frames, err := store.ReadFrames(ctx, "request-a", "", 0, 10)
	require.NoError(t, err)
	require.Len(t, frames, 1)
	require.Equal(t, frameID, frames[0].StreamID)
}

func TestKVStoreConnectionExpires(t *testing.T) {
	ctx := context.Background()
	store := NewKVStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	now := time.Now()
	store.now = func() time.Time { return now }
	require.NoError(t, store.TouchManager(ctx, "manager-a", "instance-a"))
	store.now = func() time.Time { return now.Add(kvConnectionTTL + time.Second) }
	connected, err := store.IsManagerConnected(ctx, "manager-a")
	require.NoError(t, err)
	require.False(t, connected)
}
