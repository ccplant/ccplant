package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"k8s.io/client-go/kubernetes/fake"
)

type blockingSessionListCache struct {
	mu       sync.Mutex
	cached   []portrepos.CachedSessionDTO
	setCalls int
	setStart chan struct{}
	release  chan struct{}
}

func (c *blockingSessionListCache) GetSessionListCache(context.Context, string) ([]portrepos.CachedSessionDTO, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached, nil
}

func (c *blockingSessionListCache) SetSessionListCache(_ context.Context, _ string, sessions []portrepos.CachedSessionDTO, _ time.Duration) error {
	c.mu.Lock()
	c.setCalls++
	c.mu.Unlock()
	select {
	case c.setStart <- struct{}{}:
	default:
	}
	<-c.release
	c.mu.Lock()
	c.cached = sessions
	c.mu.Unlock()
	return nil
}

func (*blockingSessionListCache) InvalidateSessionListCache(context.Context, string) error {
	return nil
}
func (*blockingSessionListCache) UpdateSessionInCache(context.Context, string, portrepos.CachedSessionDTO, time.Duration) error {
	return nil
}
func (*blockingSessionListCache) UpdateSessionStatusInCache(context.Context, string, string, string, time.Time, time.Duration) error {
	return nil
}
func (*blockingSessionListCache) DeleteSessionFromCache(context.Context, string, string, time.Duration) error {
	return nil
}

func TestListSessionsCoalescesConcurrentCacheMisses(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	client := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	cache := &blockingSessionListCache{setStart: make(chan struct{}, 1), release: make(chan struct{})}
	manager.SetSessionListCacheRepository(cache)

	const callers = 20
	var wg sync.WaitGroup
	wg.Add(callers)
	go func() {
		defer wg.Done()
		manager.ListSessions(entities.SessionFilter{})
	}()
	<-cache.setStart
	for i := 1; i < callers; i++ {
		go func() {
			defer wg.Done()
			manager.ListSessions(entities.SessionFilter{})
		}()
	}
	close(cache.release)
	wg.Wait()

	serviceLists := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "services" {
			serviceLists++
		}
	}
	if serviceLists != 1 {
		t.Fatalf("service LIST calls = %d, want 1", serviceLists)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.setCalls != 1 {
		t.Fatalf("cache writes = %d, want 1", cache.setCalls)
	}
}
