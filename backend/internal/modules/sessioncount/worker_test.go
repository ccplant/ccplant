package sessioncount

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sessionrunner "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type fakeAllocationStore struct {
	allocations []*sessionrunner.Allocation
	bindings    []*sessionrunner.Binding
}

func (f *fakeAllocationStore) ListAllocations(context.Context, string) ([]*sessionrunner.Allocation, error) {
	return f.allocations, nil
}
func (f *fakeAllocationStore) ListBindings(context.Context, string) ([]*sessionrunner.Binding, error) {
	return f.bindings, nil
}

type fakeRouteRepository struct{ routes []*portrepos.SessionRoute }

func (f *fakeRouteRepository) Save(context.Context, *portrepos.SessionRoute) error { return nil }
func (f *fakeRouteRepository) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return nil, nil
}
func (f *fakeRouteRepository) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return f.routes, nil
}
func (f *fakeRouteRepository) Delete(context.Context, string) error { return nil }

type fakeTeamRepository struct {
	teams map[string]*entities.TeamConfig
}

func (f *fakeTeamRepository) Save(context.Context, *entities.TeamConfig) error { return nil }
func (f *fakeTeamRepository) FindByTeamID(_ context.Context, id string) (*entities.TeamConfig, error) {
	return f.teams[id], nil
}
func (f *fakeTeamRepository) Delete(context.Context, string) error                 { return nil }
func (f *fakeTeamRepository) Exists(context.Context, string) (bool, error)         { return false, nil }
func (f *fakeTeamRepository) List(context.Context) ([]*entities.TeamConfig, error) { return nil, nil }

type memoryCountRepository struct {
	dimensions []entities.SessionCountDimension
	snapshots  [][]entities.SessionCountSample
}

func (r *memoryCountRepository) ListDimensions(context.Context) ([]entities.SessionCountDimension, error) {
	return append([]entities.SessionCountDimension(nil), r.dimensions...), nil
}
func (r *memoryCountRepository) SaveSnapshot(_ context.Context, _ time.Time, samples []entities.SessionCountSample) error {
	copyOfSamples := append([]entities.SessionCountSample(nil), samples...)
	r.snapshots = append(r.snapshots, copyOfSamples)
	for _, sample := range samples {
		found := false
		for _, dimension := range r.dimensions {
			if dimension == sample.SessionCountDimension {
				found = true
			}
		}
		if !found {
			r.dimensions = append(r.dimensions, sample.SessionCountDimension)
		}
	}
	return nil
}
func (r *memoryCountRepository) Close() error { return nil }

func TestWorkerCollectsByPrincipalAndWritesZeroAfterStop(t *testing.T) {
	team := entities.NewTeamConfig("org/platform", nil, nil)
	team.SetPrincipalID("team-01ABC")
	store := &fakeAllocationStore{
		allocations: []*sessionrunner.Allocation{
			{SessionID: "user-running", Pool: "linux", Status: sessionrunner.AllocationRunning},
			{SessionID: "user-stable", Pool: "linux", Status: sessionrunner.AllocationRunning},
			{SessionID: "team-pending", Pool: "linux", Status: sessionrunner.AllocationPending},
			{SessionID: "completed", Pool: "linux", Status: sessionrunner.AllocationCompleted},
		},
		bindings: []*sessionrunner.Binding{
			{Pool: "linux", SubjectType: sessionrunner.SubjectUser, SubjectID: "user-principal"},
			{Pool: "linux", SubjectType: sessionrunner.SubjectTeam, SubjectID: "org/platform"},
		},
	}
	routes := &fakeRouteRepository{routes: []*portrepos.SessionRoute{
		{SessionID: "user-running", Scope: string(entities.ScopeUser), UserID: "user-principal", Status: "running"},
		{SessionID: "user-stable", Scope: string(entities.ScopeUser), UserID: "user-principal", Status: "stable"},
		{SessionID: "user-suspended", Scope: string(entities.ScopeUser), UserID: "user-principal", Pool: "linux", Status: "suspended"},
		{SessionID: "team-pending", Scope: string(entities.ScopeTeam), TeamID: "org/platform", UserID: "creator-principal", Status: "active"},
		{SessionID: "completed", Scope: string(entities.ScopeUser), UserID: "user-principal"},
	}}
	repository := &memoryCountRepository{}
	worker := NewWorker(store, routes, &fakeTeamRepository{teams: map[string]*entities.TeamConfig{"org/platform": team}}, repository, time.Minute)
	worker.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 42, 0, time.UTC) }

	require.NoError(t, worker.Collect(context.Background()))
	require.Equal(t, []entities.SessionCountSample{
		{SessionCountDimension: entities.SessionCountDimension{Pool: "linux", PrincipalID: "team-01ABC"}, SampledAt: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), AllCount: 1, ActiveCount: 1},
		{SessionCountDimension: entities.SessionCountDimension{Pool: "linux", PrincipalID: "user-principal"}, SampledAt: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC), AllCount: 3, ActiveCount: 1, RunningCount: 1, SuspendedCount: 1},
	}, repository.snapshots[0])

	store.allocations = nil
	for _, route := range routes.routes {
		route.Status = "terminating"
	}
	worker.now = func() time.Time { return time.Date(2026, 9, 27, 12, 1, 5, 0, time.UTC) }
	require.NoError(t, worker.Collect(context.Background()))
	require.Len(t, repository.snapshots[1], 2)
	for _, sample := range repository.snapshots[1] {
		require.Zero(t, sample.AllCount)
		require.Zero(t, sample.ActiveCount)
		require.Zero(t, sample.RunningCount)
		require.Zero(t, sample.SuspendedCount)
	}
}
