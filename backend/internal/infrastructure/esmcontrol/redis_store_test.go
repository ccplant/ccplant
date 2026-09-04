package esmcontrol

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store := NewRedisStore(client)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return store, server
}

func TestRedisStoreWaitingReadsDoNotStarveFrameWrites(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 1})
	store := NewRedisStore(client)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	readCtx, cancelRead := context.WithCancel(context.Background())
	defer cancelRead()
	readDone := make(chan error, 1)
	go func() {
		_, err := store.ReadFrames(readCtx, "waiting-request", "0-0", 100*time.Millisecond, 1)
		readDone <- err
	}()

	// Let the waiter begin polling with the sole pooled connection.
	time.Sleep(25 * time.Millisecond)
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
	defer cancelWrite()
	_, err := store.AppendFrames(writeCtx, "other-request", []core.ResponseFrame{{
		ID: "frame-a", RequestID: "other-request", Done: true,
	}})
	require.NoError(t, err)

	require.NoError(t, <-readDone)
}

func TestRedisStoreConcurrentManagerTouches(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	const workers = 64
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- store.TouchManager(ctx, "manager-a", "instance-a")
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	connected, err := store.IsManagerConnected(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, connected)
}

func TestRedisStoreKeepsLongRunningRequestOwnership(t *testing.T) {
	store, server := newTestRedisStore(t)
	ctx := context.Background()

	_, err := store.EnqueueCommand(ctx, "manager-a", core.Command{ID: "request-a"})
	require.NoError(t, err)

	server.FastForward(6 * time.Minute)
	owned, err := store.RequestBelongsToManager(ctx, "request-a", "manager-a")
	require.NoError(t, err)
	require.True(t, owned)

	ttl := server.TTL(requestOwnerKey("request-a"))
	require.Greater(t, ttl, 23*time.Hour)
}

func TestRedisStoreAppendsFrameBatchInOrderAndDeduplicates(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()
	frames := []core.ResponseFrame{
		{ID: "frame-a", RequestID: "request-a", Sequence: 1, Body: []byte("one")},
		{ID: "frame-b", RequestID: "request-a", Sequence: 2, Body: []byte("two"), Done: true},
	}
	last, err := store.AppendFrames(ctx, "request-a", frames)
	require.NoError(t, err)
	require.NotEmpty(t, last)
	_, err = store.AppendFrames(ctx, "request-a", frames)
	require.NoError(t, err)

	got, err := store.ReadFrames(ctx, "request-a", "0-0", 0, 100)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].Sequence)
	require.Equal(t, int64(2), got[1].Sequence)
}

func TestRedisStoreAckDoesNotMoveBackward(t *testing.T) {
	store, server := newTestRedisStore(t)
	ctx := context.Background()
	require.NoError(t, store.AckCommand(ctx, "manager-a", "10-2"))
	require.NoError(t, store.AckCommand(ctx, "manager-a", "9-99"))
	ack, err := server.Get(commandAckKey("manager-a"))
	require.NoError(t, err)
	require.Equal(t, "10-2", ack)
}

func TestRedisStoreDoesNotRefreshOwnershipForWrongManager(t *testing.T) {
	store, server := newTestRedisStore(t)
	ctx := context.Background()

	_, err := store.EnqueueCommand(ctx, "manager-a", core.Command{ID: "request-a"})
	require.NoError(t, err)
	server.FastForward(time.Hour)
	wantTTL := server.TTL(requestOwnerKey("request-a"))

	owned, err := store.RequestBelongsToManager(ctx, "request-a", "manager-b")
	require.NoError(t, err)
	require.False(t, owned)
	require.Equal(t, wantTTL, server.TTL(requestOwnerKey("request-a")))
}
