package sessioncontrol

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
)

func TestAppendEventsUsesSingleDedupKeyPerSession(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	store := NewRedisStore(client)
	ctx := context.Background()

	events := []core.Event{{ID: "event-a"}, {ID: "event-b"}}
	_, err := store.AppendEvents(ctx, "session-a", events)
	require.NoError(t, err)
	_, err = store.AppendEvents(ctx, "session-a", events)
	require.NoError(t, err)

	got, err := store.ReadEvents(ctx, "session-a", "0-0", 0, 100)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.ElementsMatch(t, []string{eventKey("session-a"), eventDedupKey("session-a")}, server.Keys())
	require.Positive(t, server.TTL(eventDedupKey("session-a")))
}

func TestStreamIDLess(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        bool
	}{
		{name: "empty cursor", left: "", right: "10-0", want: true},
		{name: "milliseconds", left: "9-9", right: "10-0", want: true},
		{name: "sequence", left: "10-1", right: "10-2", want: true},
		{name: "equal", left: "10-2", right: "10-2", want: false},
		{name: "newer", left: "11-0", right: "10-9", want: false},
		{name: "missing right", left: "10-0", right: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamIDLess(test.left, test.right); got != test.want {
				t.Fatalf("streamIDLess(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}
