package sessioncontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
)

func TestMemoryStoreCommandLifecycle(t *testing.T) {
	store := NewMemoryStore()
	id, err := store.EnqueueCommand(context.Background(), "session", core.Command{ID: "command", Type: "stop"})
	require.NoError(t, err)

	commands, err := store.ReadCommands(context.Background(), "session", "", 0, 100)
	require.NoError(t, err)
	require.Len(t, commands, 1)
	require.Equal(t, id, commands[0].StreamID)
	require.NoError(t, store.AckCommand(context.Background(), "session", id))

	commands, err = store.ReadCommands(context.Background(), "session", "", 0, 100)
	require.NoError(t, err)
	require.Empty(t, commands)
}

func TestMemoryStoreConnectionAndEventDeduplication(t *testing.T) {
	store := NewMemoryStore()
	require.NoError(t, store.TouchConnection(context.Background(), "session"))
	connected, err := store.IsConnected(context.Background(), "session")
	require.NoError(t, err)
	require.True(t, connected)

	event := core.Event{ID: "event", Type: "status", CreatedAt: time.Now()}
	first, err := store.AppendEvents(context.Background(), "session", []core.Event{event})
	require.NoError(t, err)
	second, err := store.AppendEvents(context.Background(), "session", []core.Event{event})
	require.NoError(t, err)
	require.NotEmpty(t, first)
	require.Empty(t, second)

	events, err := store.ReadEvents(context.Background(), "session", "", 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
}
