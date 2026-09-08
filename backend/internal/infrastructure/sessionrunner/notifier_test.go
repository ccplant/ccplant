package sessionrunner

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisAllocationNotifierDeliversOnlyToMatchingPool(t *testing.T) {
	server := miniredis.RunT(t)
	publisherClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	subscriberClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = publisherClient.Close()
		_ = subscriberClient.Close()
	})
	publisher := NewRedisAllocationNotifier(publisherClient)
	subscriber := NewRedisAllocationNotifier(subscriberClient)
	ctx := context.Background()
	managed, cancelManaged, err := subscriber.Subscribe(ctx, "managed")
	if err != nil {
		t.Fatal(err)
	}
	defer cancelManaged()
	other, cancelOther, err := subscriber.Subscribe(ctx, "other")
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOther()

	if err := publisher.Notify(ctx, "managed"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-managed:
	case <-time.After(time.Second):
		t.Fatal("matching pool did not receive Redis notification")
	}
	select {
	case <-other:
		t.Fatal("different pool received Redis notification")
	case <-time.After(25 * time.Millisecond):
	}
}
