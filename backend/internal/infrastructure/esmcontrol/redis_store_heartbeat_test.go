package esmcontrol

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeartbeatIsFresh(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	recorded := func(age time.Duration) string {
		return strconv.FormatInt(now.Add(-age).UnixMilli(), 10)
	}

	require.True(t, heartbeatIsFresh(recorded(0), now, heartbeatFreshness))
	require.True(t, heartbeatIsFresh(recorded(heartbeatFreshness), now, heartbeatFreshness))
	require.False(t, heartbeatIsFresh(recorded(heartbeatFreshness+time.Millisecond), now, heartbeatFreshness))
	require.False(t, heartbeatIsFresh("not-a-timestamp", now, heartbeatFreshness))
}

func TestRedisStoreSchedulableRequiresRecentHeartbeatOnceRecorded(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestRedisStore(t)
	now := time.Now()
	store.now = func() time.Time { return now }

	schedulable, err := store.IsManagerSchedulable(ctx, "manager-a")
	require.NoError(t, err)
	require.False(t, schedulable, "a manager without a control connection is never schedulable")

	require.NoError(t, store.TouchManager(ctx, "manager-a", "instance-a"))
	schedulable, err = store.IsManagerSchedulable(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, schedulable, "a connected manager without any heartbeat record keeps the previous behavior")

	require.NoError(t, store.RecordManagerHeartbeat(ctx, "manager-a"))
	schedulable, err = store.IsManagerSchedulable(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, schedulable)

	now = now.Add(heartbeatFreshness + time.Second)
	require.NoError(t, store.TouchManager(ctx, "manager-a", "instance-a"))
	connected, err := store.IsManagerConnected(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, connected, "command polling keeps the control connection alive")
	schedulable, err = store.IsManagerSchedulable(ctx, "manager-a")
	require.NoError(t, err)
	require.False(t, schedulable, "a stale heartbeat must stop new placements even while commands are still polled")

	require.NoError(t, store.RecordManagerHeartbeat(ctx, "manager-a"))
	schedulable, err = store.IsManagerSchedulable(ctx, "manager-a")
	require.NoError(t, err)
	require.True(t, schedulable, "the next completed heartbeat restores scheduling")
}
