package repositories

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestLibSQLSessionCountRepositoryAppendsStatusEventsIdempotently(t *testing.T) {
	ctx := context.Background()
	repository, err := NewSessionCountRepository(ctx, "libsql", "file:"+filepath.Join(t.TempDir(), "counts.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	event := entities.SessionStatusUsageEvent{EventID: "event-1", OccurredAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), SessionID: "session-1", Pool: "linux", Scope: "team", PrincipalID: "team-01ABC", Status: "active"}
	require.NoError(t, repository.SaveEvent(ctx, event))
	require.NoError(t, repository.SaveEvent(ctx, event))

	concrete := repository.(*LibSQLSessionCountRepository)
	var rows int
	var status string
	require.NoError(t, concrete.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(status) FROM agentapi_session_status_events`).Scan(&rows, &status))
	require.Equal(t, 1, rows)
	require.Equal(t, "active", status)
}

func TestNewSessionCountRepositoryRejectsUnknownBackend(t *testing.T) {
	_, err := NewSessionCountRepository(context.Background(), "postgres", "", "")
	require.EqualError(t, err, `unsupported session count backend "postgres"`)
}

func TestLibSQLSessionCountRepositoryListsCarryForwardAndRangeEvents(t *testing.T) {
	ctx := context.Background()
	repository, err := NewSessionCountRepository(ctx, "libsql", "file:"+filepath.Join(t.TempDir(), "counts.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	runtimeRepo := repository.(portrepos.SessionRuntimeRepository)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	events := []entities.SessionStatusUsageEvent{
		{EventID: "before", OccurredAt: base.Add(-time.Hour), SessionID: "session-1", Pool: "linux", PrincipalID: "user-1", Status: "active"},
		{EventID: "inside", OccurredAt: base.Add(time.Hour), SessionID: "session-1", Pool: "linux", PrincipalID: "user-1", Status: "running"},
		{EventID: "after", OccurredAt: base.Add(25 * time.Hour), SessionID: "session-1", Pool: "linux", PrincipalID: "user-1", Status: "suspended"},
		{EventID: "other", OccurredAt: base, SessionID: "session-2", Pool: "linux", PrincipalID: "user-2", Status: "active"},
	}
	for _, event := range events {
		require.NoError(t, repository.SaveEvent(ctx, event))
	}
	got, err := runtimeRepo.ListRuntimeEvents(ctx, entities.SessionRuntimeQuery{PrincipalID: "user-1", From: base, To: base.Add(24 * time.Hour)})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []string{"before", "inside"}, []string{got[0].EventID, got[1].EventID})
	coverage, err := runtimeRepo.RuntimeCoverageStart(ctx, "user-1")
	require.NoError(t, err)
	require.NotNil(t, coverage)
	require.True(t, coverage.Equal(base.Add(-time.Hour)))
}
