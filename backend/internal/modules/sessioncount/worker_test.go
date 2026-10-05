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

type fakeSource struct {
	session entities.Session
	events  chan portrepos.SessionStatusEvent
}

func (s *fakeSource) GetSession(string) entities.Session                     { return s.session }
func (s *fakeSource) ListSessions(entities.SessionFilter) []entities.Session { return nil }
func (s *fakeSource) SubscribeStatusEvents() (<-chan portrepos.SessionStatusEvent, func()) {
	return s.events, func() {}
}

type fakeAllocations struct{ allocation *sessionrunner.Allocation }

func (s *fakeAllocations) GetAllocation(context.Context, string) (*sessionrunner.Allocation, error) {
	return s.allocation, nil
}

type fakeRoutes struct{ route *portrepos.SessionRoute }

func (r *fakeRoutes) Save(context.Context, *portrepos.SessionRoute) error { return nil }
func (r *fakeRoutes) Get(context.Context, string) (*portrepos.SessionRoute, error) {
	return r.route, nil
}
func (r *fakeRoutes) List(context.Context, string) ([]*portrepos.SessionRoute, error) {
	return nil, nil
}
func (r *fakeRoutes) Delete(context.Context, string) error { return nil }

type fakeTeams struct{ team *entities.TeamConfig }

func (r *fakeTeams) Save(context.Context, *entities.TeamConfig) error { return nil }
func (r *fakeTeams) FindByTeamID(context.Context, string) (*entities.TeamConfig, error) {
	return r.team, nil
}
func (r *fakeTeams) Delete(context.Context, string) error                 { return nil }
func (r *fakeTeams) Exists(context.Context, string) (bool, error)         { return false, nil }
func (r *fakeTeams) List(context.Context) ([]*entities.TeamConfig, error) { return nil, nil }

type memoryRepository struct {
	events []entities.SessionStatusUsageEvent
}

func (r *memoryRepository) SaveEvent(_ context.Context, event entities.SessionStatusUsageEvent) error {
	r.events = append(r.events, event)
	return nil
}
func (r *memoryRepository) Close() error { return nil }

func TestRecordStatusResolvesTeamPrincipalAndPool(t *testing.T) {
	team := entities.NewTeamConfig("org/platform", nil, nil)
	team.SetPrincipalID("team-01ABC")
	repo := &memoryRepository{}
	worker := NewWorker(&fakeSource{}, &fakeAllocations{allocation: &sessionrunner.Allocation{Pool: "linux"}}, &fakeRoutes{route: &portrepos.SessionRoute{SessionID: "session-1", Scope: "team", TeamID: "org/platform"}}, &fakeTeams{team: team}, repo)
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	require.NoError(t, worker.RecordStatus(context.Background(), portrepos.SessionStatusEvent{SessionID: "session-1", Status: "running", Timestamp: at}))
	require.Len(t, repo.events, 1)
	require.Equal(t, "team-01ABC", repo.events[0].PrincipalID)
	require.Equal(t, "linux", repo.events[0].Pool)
	require.Equal(t, "running", repo.events[0].Status)
	require.Equal(t, at, repo.events[0].OccurredAt)
}

func TestRecordResolvedStatusAfterMetadataDeletion(t *testing.T) {
	repo := &memoryRepository{}
	routes := &fakeRoutes{route: &portrepos.SessionRoute{
		SessionID: "session-1", Pool: "linux", Scope: "user", UserID: "user-1",
	}}
	worker := NewWorker(&fakeSource{}, &fakeAllocations{}, routes, &fakeTeams{}, repo)
	dimensions, err := worker.ResolveDimensions(context.Background(), "session-1")
	require.NoError(t, err)

	routes.route = nil
	at := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	require.NoError(t, worker.RecordResolvedStatus(context.Background(), dimensions, portrepos.SessionStatusEvent{
		SessionID: "session-1", Status: "terminated", Timestamp: at,
	}))

	require.Len(t, repo.events, 1)
	require.Equal(t, "terminated", repo.events[0].Status)
	require.Equal(t, "linux", repo.events[0].Pool)
	require.Equal(t, "user-1", repo.events[0].PrincipalID)
	require.Equal(t, at, repo.events[0].OccurredAt)
}
