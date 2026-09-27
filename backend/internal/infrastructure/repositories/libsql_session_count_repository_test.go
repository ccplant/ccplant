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
		AllCount:              3,
		ActiveCount:           2,
		RunningCount:          1,
		SuspendedCount:        4,
	}}))
	// A retry for the same collection bucket is idempotent.
	require.NoError(t, repository.SaveSnapshot(ctx, sampledAt, []entities.SessionCountSample{{
		SessionCountDimension: dimension,
		SampledAt:             sampledAt,
		AllCount:              3,
		ActiveCount:           2,
		RunningCount:          1,
		SuspendedCount:        4,
	}}))
	// The transition to zero is recorded.
	require.NoError(t, repository.SaveSnapshot(ctx, sampledAt.Add(time.Minute), []entities.SessionCountSample{{
		SessionCountDimension: dimension,
		SampledAt:             sampledAt.Add(time.Minute),
		ActiveCount:           0,
		RunningCount:          0,
	}}))
	// An unchanged zero in a later collection bucket is deliberately omitted.
	require.NoError(t, repository.SaveSnapshot(ctx, sampledAt.Add(2*time.Minute), []entities.SessionCountSample{{
		SessionCountDimension: dimension,
		SampledAt:             sampledAt.Add(2 * time.Minute),
		ActiveCount:           0,
		RunningCount:          0,
	}}))

	dimensions, err := repository.ListDimensions(ctx)
	require.NoError(t, err)
	require.Equal(t, []entities.SessionCountDimension{dimension}, dimensions)

	concrete := repository.(*LibSQLSessionCountRepository)
	var rows, all, active, running, suspended int
	require.NoError(t, concrete.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agentapi_session_count_samples`).Scan(&rows))
	require.NoError(t, concrete.db.QueryRowContext(ctx, `SELECT all_count,active_count,running_count,suspended_count FROM agentapi_session_count_samples ORDER BY sampled_at DESC LIMIT 1`).Scan(&all, &active, &running, &suspended))
	require.Equal(t, 2, rows)
	require.Zero(t, all)
	require.Zero(t, active)
	require.Zero(t, running)
	require.Zero(t, suspended)
}

func TestNewSessionCountRepositoryRejectsUnknownBackend(t *testing.T) {
	_, err := NewSessionCountRepository(context.Background(), "postgres", "", "")
	require.EqualError(t, err, `unsupported session count backend "postgres"`)
}
