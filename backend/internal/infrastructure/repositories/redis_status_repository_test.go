package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestRouteStatusChannelIsSeparateFromManagerChannel(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	repo := NewRedisStatusRepository(client, "pod")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	global, err := repo.SubscribeGlobal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	route, err := repo.SubscribeRouteStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := portrepos.StatusChangeEvent{SessionID: "s1", Status: "active", UpdatedAt: time.Now().UTC(), PodID: "origin"}
	if err := repo.PublishRouteStatusChange(ctx, want); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-route:
		if got.SessionID != want.SessionID || got.Status != want.Status || got.PodID != want.PodID || !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatalf("route event = %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("route subscriber received nothing")
	}
	select {
	case got := <-global:
		t.Fatalf("manager channel received route event %+v", got)
	case <-time.After(300 * time.Millisecond):
	}
}
