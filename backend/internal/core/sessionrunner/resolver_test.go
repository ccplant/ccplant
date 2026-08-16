package sessionrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resolverStore struct {
	Store
	managers    []*Manager
	pools       []*LogicalPool
	suppliers   []*PoolSupplier
	bindings    []*Binding
	preferences map[string]*Preference
}

func (s *resolverStore) ListManagers(context.Context) ([]*Manager, error) { return s.managers, nil }
func (s *resolverStore) ListLogicalPools(context.Context) ([]*LogicalPool, error) {
	return s.pools, nil
}
func (s *resolverStore) ListPoolSuppliers(context.Context) ([]*PoolSupplier, error) {
	return s.suppliers, nil
}
func (s *resolverStore) ListBindings(context.Context, string) ([]*Binding, error) {
	return s.bindings, nil
}
func (s *resolverStore) GetPreference(_ context.Context, kind SubjectType, id string) (*Preference, error) {
	if value := s.preferences[string(kind)+":"+id]; value != nil {
		return value, nil
	}
	return nil, ErrNotFound
}

func TestResolverRequiresBindingAndSelectsPool(t *testing.T) {
	now := time.Now().UTC()
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true, LastHeartbeatAt: now}},
		pools:       []*LogicalPool{{Name: "linux", Enabled: true, Labels: map[string]string{"arch": "amd64"}}},
		suppliers:   []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectTeam, SubjectID: "org/team", Enabled: true}},
		preferences: map[string]*Preference{},
	}
	resolver := NewResolver(store, time.Minute)
	resolver.now = func() time.Time { return now }
	pool, err := resolver.Resolve(context.Background(), Subject{Type: SubjectTeam, ID: "org/team"}, map[string]string{"allocator.pool": "linux", "allocator.arch": "amd64"})
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Pool.Name)
	require.Equal(t, SubjectTeam, pool.Binding.SubjectType)

	_, err = resolver.Resolve(context.Background(), Subject{Type: SubjectUser, ID: "bob"}, map[string]string{"allocator.pool": "linux"})
	require.Error(t, err)
}

func TestResolverUsesUserPreference(t *testing.T) {
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true}},
		pools:       []*LogicalPool{{Name: "linux", Enabled: true, IsDefault: true}},
		suppliers:   []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}},
		preferences: map[string]*Preference{"user:alice": {SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, DefaultPool: "linux"}},
	}
	pool, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Pool.Name)
}

func TestResolverAllowsClusterWideBinding(t *testing.T) {
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true}},
		pools:       []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers:   []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectAll, Enabled: true}},
		preferences: map[string]*Preference{},
	}

	pool, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "any-user"}, map[string]string{"allocator.pool": "linux"})
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Pool.Name)

	available, err := NewResolver(store, 0).AvailablePools(context.Background(), Subject{Type: SubjectTeam, ID: "any/team"})
	require.NoError(t, err)
	require.Len(t, available, 1)
}

func TestResolverReturnsLogicalPoolOnceForMultipleSuppliers(t *testing.T) {
	store := &resolverStore{
		managers: []*Manager{{ID: "manager-a", Enabled: true}, {ID: "manager-b", Enabled: true}},
		pools:    []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{
			{Pool: "linux", ManagerID: "manager-a", Enabled: true},
			{Pool: "linux", ManagerID: "manager-b", Enabled: true},
		},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}},
		preferences: map[string]*Preference{},
	}
	pools, err := NewResolver(store, 0).AvailablePools(context.Background(), Subject{Type: SubjectUser, ID: "alice"})
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Equal(t, "linux", pools[0].Name)
}

func TestResolverPrefersExactBindingOverAll(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true, IsDefault: true}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings: []*Binding{
			{ID: "binding-all", Pool: "linux", SubjectType: SubjectAll, Enabled: true, MaxConcurrent: 100},
			{ID: "binding-alice", Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, MaxConcurrent: 2},
		},
		preferences: map[string]*Preference{},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Equal(t, "binding-alice", resolved.Binding.ID)
	require.Equal(t, 2, resolved.Binding.MaxConcurrent)

	resolved, err = NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "bob"}, nil)
	require.NoError(t, err)
	require.Equal(t, "binding-all", resolved.Binding.ID)
}

func TestResolverDoesNotUseBindingsFromAnotherScope(t *testing.T) {
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true}},
		pools:       []*LogicalPool{{Name: "linux", Enabled: true, IsDefault: true}},
		suppliers:   []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:    []*Binding{{ID: "binding-team", Pool: "linux", SubjectType: SubjectTeam, SubjectID: "org/team", Enabled: true}},
		preferences: map[string]*Preference{},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Nil(t, resolved)

	store.bindings = []*Binding{{ID: "binding-user", Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}}
	resolved, err = NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectTeam, ID: "org/team"}, nil)
	require.NoError(t, err)
	require.Nil(t, resolved)
}
