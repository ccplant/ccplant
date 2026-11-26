package repositories

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type orderedAtomicStore struct {
	kvstore.Store
	backend kvstore.AtomicMutator

	mu        sync.Mutex
	arrived   int
	allReady  chan struct{}
	firstDone chan struct{}
}

func newOrderedAtomicStore(store kvstore.Store) *orderedAtomicStore {
	return &orderedAtomicStore{
		Store: store, backend: store.(kvstore.AtomicMutator),
		allReady: make(chan struct{}), firstDone: make(chan struct{}),
	}
}

func (s *orderedAtomicStore) ApplyAtomically(ctx context.Context, mutations []kvstore.Mutation) ([]kvstore.Record, error) {
	s.mu.Lock()
	s.arrived++
	order := s.arrived
	if s.arrived == 2 {
		close(s.allReady)
	}
	s.mu.Unlock()
	<-s.allReady
	if order == 2 {
		<-s.firstDone
	}
	result, err := s.backend.ApplyAtomically(ctx, mutations)
	if order == 1 {
		close(s.firstDone)
	}
	return result, err
}

func TestLibSQLSessionProfileCASRejectsConcurrentCycle(t *testing.T) {
	ctx := context.Background()
	store, err := kvstore.NewLibSQLStore(ctx, "file://"+filepath.Join(t.TempDir(), "profiles.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	client := kvstore.NewKubernetesAdapter(fake.NewSimpleClientset(), store)
	setup := NewLibSQLSessionProfileRepository(client, "test", store)

	p1 := entities.NewSessionProfile("p1", "Profile 1", "alice")
	p2 := entities.NewSessionProfile("p2", "Profile 2", "alice")
	require.NoError(t, setup.Create(ctx, p1))
	require.NoError(t, setup.Create(ctx, p2))

	first, err := setup.Get(ctx, "p1")
	require.NoError(t, err)
	firstConfig := first.Config()
	firstConfig.SetSourceSessionProfileID("p2")
	first.SetConfig(firstConfig)
	second, err := setup.Get(ctx, "p2")
	require.NoError(t, err)
	secondConfig := second.Config()
	secondConfig.SetSourceSessionProfileID("p1")
	second.SetConfig(secondConfig)

	ordered := newOrderedAtomicStore(store)
	repo1 := NewLibSQLSessionProfileRepository(client, "test", ordered)
	repo2 := NewLibSQLSessionProfileRepository(client, "test", ordered)
	errorsByUpdate := make(chan error, 2)
	go func() { errorsByUpdate <- repo1.Update(ctx, first) }()
	go func() { errorsByUpdate <- repo2.Update(ctx, second) }()

	err1, err2 := <-errorsByUpdate, <-errorsByUpdate
	require.True(t, (err1 == nil) != (err2 == nil), "exactly one update must commit: %v / %v", err1, err2)
	if err1 != nil {
		require.True(t, errors.Is(err1, portrepos.ErrSessionProfileGraphConflict), err1)
	}
	if err2 != nil {
		require.True(t, errors.Is(err2, portrepos.ErrSessionProfileGraphConflict), err2)
	}

	loaded1, err := setup.Get(ctx, "p1")
	require.NoError(t, err)
	loaded2, err := setup.Get(ctx, "p2")
	require.NoError(t, err)
	cfg1, cfg2 := loaded1.Config(), loaded2.Config()
	require.False(t, cfg1.SourceSessionProfileID() == "p2" && cfg2.SourceSessionProfileID() == "p1")
	referencedID := "p1"
	if cfg1.SourceSessionProfileID() == "p2" {
		referencedID = "p2"
	}
	err = setup.Delete(ctx, referencedID)
	require.ErrorIs(t, err, portrepos.ErrSessionProfileGraphConflict)
	_, err = setup.Get(ctx, referencedID)
	require.NoError(t, err, "a referenced profile must not be partially deleted")
}
