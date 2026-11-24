package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

type startupCleanupStore struct {
	kvstore.Store
	steps *[]string
}

func (s startupCleanupStore) Close() error {
	*s.steps = append(*s.steps, "store")
	return errors.New("test store close failure")
}

type startupCleanupManager struct{ steps *[]string }

func (m startupCleanupManager) Shutdown(time.Duration) error {
	*m.steps = append(*m.steps, "manager")
	return errors.New("test manager shutdown failure")
}

func TestSessionManagerStartupCleanupContinuesAfterCloseErrors(t *testing.T) {
	var steps []string
	client := redis.NewClient(&redis.Options{Addr: "unused.invalid:6379"})
	cleanupSessionManagerStartup(startupCleanupManager{steps: &steps}, startupCleanupStore{steps: &steps}, client,
		func() { steps = append(steps, "cancel") })
	if !reflect.DeepEqual(steps, []string{"cancel", "store", "manager"}) {
		t.Fatalf("cleanup order = %v", steps)
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, redis.ErrClosed) {
		t.Fatalf("Redis after cleanup = %v, want closed", err)
	}
}

func TestSessionManagerStartupCleanupAcceptsUninitializedResources(t *testing.T) {
	var steps []string
	cleanupSessionManagerStartup(startupCleanupManager{steps: &steps}, nil, nil, nil)
	if !reflect.DeepEqual(steps, []string{"manager"}) {
		t.Fatalf("cleanup steps = %v", steps)
	}
}
