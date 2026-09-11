package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestUpdateSessionStatusInCachePreservesSessionMetadata(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	repo := NewRedisStatusRepository(client, "test-pod")
	ctx := context.Background()
	key := redisSessionListCachePrefix + "test-ns:all"
	original := portrepos.CachedSessionDTO{
		ID:          "session-1",
		UserID:      "user-1",
		Status:      "starting",
		Description: "keep me",
	}
	if err := repo.SetSessionListCache(ctx, key, []portrepos.CachedSessionDTO{original}, time.Minute); err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Now().UTC().Truncate(time.Nanosecond)
	if err := repo.UpdateSessionStatusInCache(ctx, "test-ns", original.ID, "active", updatedAt, time.Minute); err != nil {
		t.Fatal(err)
	}
	cached, err := repo.GetSessionListCache(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0].Status != "active" {
		t.Fatalf("cached sessions = %#v, want active session", cached)
	}
	if cached[0].UserID != original.UserID || cached[0].Description != original.Description {
		t.Fatalf("metadata changed: %#v", cached[0])
	}
	if !cached[0].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at = %v, want %v", cached[0].UpdatedAt, updatedAt)
	}
}
