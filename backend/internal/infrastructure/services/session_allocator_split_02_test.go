package services

import (
	"context"
	"testing"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeletePendingSessionAllocation(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	client := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	ctx := context.Background()
	if err := manager.saveSessionAllocation(ctx, &sessionallocation.AllocationRequest{
		SessionID: "pending-session",
		Request:   &entities.RunServerRequest{UserID: "test-user"},
		Status:    sessionallocation.StatusPending,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	deleted, err := manager.DeletePendingSessionAllocation(ctx, "pending-session")
	if err != nil {
		t.Fatalf("DeletePendingSessionAllocation() error = %v", err)
	}
	if !deleted {
		t.Fatal("DeletePendingSessionAllocation() deleted=false, want true")
	}
	_, err = client.CoreV1().Secrets("test-ns").Get(
		ctx,
		sessionAllocationSecretName("pending-session"),
		metav1.GetOptions{},
	)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("allocation Secret still exists or returned unexpected error: %v", err)
	}
}

func TestDeletePendingSessionAllocationDeletesClaimedOrphan(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	client := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	ctx := context.Background()
	if err := manager.saveSessionAllocation(ctx, &sessionallocation.AllocationRequest{
		SessionID: "allocating-session",
		Request:   &entities.RunServerRequest{UserID: "test-user"},
		Status:    sessionallocation.StatusAllocating,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	deleted, err := manager.DeletePendingSessionAllocation(ctx, "allocating-session")
	if err != nil {
		t.Fatalf("DeletePendingSessionAllocation() error = %v", err)
	}
	if !deleted {
		t.Fatal("DeletePendingSessionAllocation() deleted=false, want true")
	}
	if _, err := client.CoreV1().Secrets("test-ns").Get(
		ctx,
		sessionAllocationSecretName("allocating-session"),
		metav1.GetOptions{},
	); !apierrors.IsNotFound(err) {
		t.Fatalf("allocation Secret still exists or returned unexpected error: %v", err)
	}
}

func TestDeletePendingSessionAllocationDeletesFailedAllocation(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	ctx := context.Background()
	if err := manager.saveSessionAllocation(ctx, &sessionallocation.AllocationRequest{
		SessionID: "failed-session",
		Request:   &entities.RunServerRequest{UserID: "test-user"},
		Status:    sessionallocation.StatusError,
		Message:   "failed",
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	deleted, err := manager.DeletePendingSessionAllocation(ctx, "failed-session")
	if err != nil || !deleted {
		t.Fatalf("DeletePendingSessionAllocation() = (%t, %v), want (true, nil)", deleted, err)
	}
}

func TestListSessionsIncludesAllocatingSessionAllocation(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request: &entities.RunServerRequest{
			UserID: "test-user",
			Scope:  entities.ScopeUser,
			Tags:   map[string]string{"purpose": "test"},
		},
		Status: sessionallocation.StatusAllocating,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	sessions := manager.ListSessions(entities.SessionFilter{
		UserID: "test-user",
		Status: "allocating",
		Tags:   map[string]string{"purpose": "test"},
		Scope:  entities.ScopeUser,
	})

	if len(sessions) != 1 {
		t.Fatalf("ListSessions() returned %d sessions, want 1", len(sessions))
	}
	if sessions[0].ID() != "test-session" {
		t.Fatalf("session.ID() = %q, want test-session", sessions[0].ID())
	}
	if sessions[0].Status() != "allocating" {
		t.Fatalf("session.Status() = %q, want allocating", sessions[0].Status())
	}
}

func TestSessionAllocationInvalidatesSessionListCache(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	cache := &recordingSessionListCacheRepo{}
	manager.SetSessionListCacheRepository(cache)

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request:   &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser},
		Status:    sessionallocation.StatusPending,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}
	if cache.invalidations != 1 {
		t.Fatalf("invalidations after save = %d, want 1", cache.invalidations)
	}

	if err := manager.deleteSessionAllocation(context.Background(), "test-session"); err != nil {
		t.Fatalf("deleteSessionAllocation() error = %v", err)
	}
	if cache.invalidations != 2 {
		t.Fatalf("invalidations after delete = %d, want 2", cache.invalidations)
	}
}

func TestListSessionsDoesNotUseRedisListCacheWithInformer(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	cache := &recordingSessionListCacheRepo{}
	manager.SetSessionListCacheRepository(cache)

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request: &entities.RunServerRequest{
			UserID: "test-user",
			Scope:  entities.ScopeUser,
			Tags:   map[string]string{"purpose": "test"},
		},
		Status: sessionallocation.StatusPending,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	sessions := manager.ListSessions(entities.SessionFilter{
		UserID: "test-user",
		Tags:   map[string]string{"purpose": "test"},
		Scope:  entities.ScopeUser,
	})
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() returned %d sessions, want 1", len(sessions))
	}
	if cache.setCalls != 0 {
		t.Fatalf("SetSessionListCache calls = %d, want 0", cache.setCalls)
	}
}

type recordingSessionListCacheRepo struct {
	setCalls      int
	invalidations int
}

func (r *recordingSessionListCacheRepo) SetSessionListCache(context.Context, string, []portrepos.CachedSessionDTO, time.Duration) error {
	r.setCalls++
	return nil
}

func (r *recordingSessionListCacheRepo) GetSessionListCache(context.Context, string) ([]portrepos.CachedSessionDTO, error) {
	return nil, nil
}

func (r *recordingSessionListCacheRepo) InvalidateSessionListCache(context.Context, string) error {
	r.invalidations++
	return nil
}

func (r *recordingSessionListCacheRepo) UpdateSessionInCache(context.Context, string, portrepos.CachedSessionDTO, time.Duration) error {
	return nil
}

func (r *recordingSessionListCacheRepo) DeleteSessionFromCache(context.Context, string, string, time.Duration) error {
	return nil
}

type subscribeHookNotifier struct {
	onSubscribe func()
}

func (n *subscribeHookNotifier) Notify(context.Context) error {
	return nil
}

func (n *subscribeHookNotifier) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	if n.onSubscribe != nil {
		n.onSubscribe()
		n.onSubscribe = nil
	}
	ch := make(chan struct{})
	return ch, func() { close(ch) }, nil
}
