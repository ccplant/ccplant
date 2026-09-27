package repositories

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestLibSQLSessionCountRepositoryUpsertsAndListsDimensions(t *testing.T) {
	ctx := context.Background()
	repository, err := NewSessionCountRepository(ctx, "libsql", "file:"+filepath.Join(t.TempDir(), "counts.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })

	sampledAt := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	dimension := entities.SessionCountDimension{Pool: "linux", PrincipalID: "team-01ABC"}
	require.NoError(t, repository.SaveSnapshot(ctx, sampledAt, []entities.SessionCountSample{{
		SessionCountDimension: dimension,
		SampledAt:             sampledAt,
		ActiveCount:           2,
		RunningCount:          1,
	}}))
	// A retry for the same collection bucket replaces counts instead of
	// creating a duplicate row.
	require.NoError(t, repository.SaveSnapshot(ctx, sampledAt, []entities.SessionCountSample{{
		SessionCountDimension: dimension,
		SampledAt:             sampledAt,
		ActiveCount:           0,
		RunningCount:          0,
	}}))

	dimensions, err := repository.ListDimensions(ctx)
	require.NoError(t, err)
	require.Equal(t, []entities.SessionCountDimension{dimension}, dimensions)

	concrete := repository.(*LibSQLSessionCountRepository)
	var rows, active, running int
	require.NoError(t, concrete.db.QueryRowContext(ctx, `SELECT COUNT(*), active_count, running_count FROM agentapi_session_count_samples`).Scan(&rows, &active, &running))
	require.Equal(t, 1, rows)
	require.Zero(t, active)
	require.Zero(t, running)
}

func TestNewSessionCountRepositoryRejectsUnknownBackend(t *testing.T) {
	_, err := NewSessionCountRepository(context.Background(), "postgres", "", "")
	require.EqualError(t, err, `unsupported session count backend "postgres"`)
}
