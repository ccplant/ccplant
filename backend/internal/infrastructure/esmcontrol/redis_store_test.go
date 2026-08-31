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
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return NewRedisStore(client), server
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

func TestRedisStoreManagerReconcileRevisionIsDurable(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	initial, err := store.CurrentManagerReconcileRevision(ctx, "manager-a")
	require.NoError(t, err)
	require.Equal(t, "0-0", initial)

	signaled, err := store.SignalManagerReconcile(ctx, "manager-a")
	require.NoError(t, err)
	require.NotEqual(t, initial, signaled)

	observed, err := store.WaitManagerReconcile(ctx, "manager-a", initial, time.Second)
	require.NoError(t, err)
	require.Equal(t, signaled, observed)
}

func TestRedisStoreTouchManagerForCoversLongPoll(t *testing.T) {
	store, server := newTestRedisStore(t)
	require.NoError(t, store.TouchManagerFor(context.Background(), "manager-a", "runner", 5*time.Minute+30*time.Second))
	require.GreaterOrEqual(t, server.TTL(connectionKey("manager-a")), 5*time.Minute)
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
