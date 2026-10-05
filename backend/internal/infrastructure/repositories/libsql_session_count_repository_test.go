package repositories

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
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
