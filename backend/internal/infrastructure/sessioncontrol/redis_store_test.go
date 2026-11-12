package sessioncontrol

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisStoreBlockingReadsDoNotStarveCommands(t *testing.T) {
	server := miniredis.RunT(t)
	commandClient := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 1})
	blockingClient := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 1})
	t.Cleanup(func() {
		require.NoError(t, commandClient.Close())
		require.NoError(t, blockingClient.Close())
	})
	store := NewRedisStoreWithBlockingClient(commandClient, blockingClient)

	readCtx, cancelRead := context.WithCancel(context.Background())
	defer cancelRead()
	readDone := make(chan error, 1)
	go func() {
		_, err := store.ReadCommands(readCtx, "waiting-session", "0-0", time.Second, 1)
		readDone <- err
	}()

	time.Sleep(25 * time.Millisecond)
	commandCtx, cancelCommand := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelCommand()
	connected, err := store.IsConnected(commandCtx, "other-session")
	require.NoError(t, err)
	require.False(t, connected)

	cancelRead()
	require.NoError(t, <-readDone)
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
