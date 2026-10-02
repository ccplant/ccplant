package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesTeamMembershipRepositoryPersistsAndRateLimitsSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewKubernetesTeamMembershipRepository(fake.NewSimpleClientset(), "test")
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	leased, err := repo.AcquireSync(ctx, "team-01ARZ3NDEKTSV4RRFFQ69G5FAV", "operation-1", now)
	require.NoError(t, err)
	require.Equal(t, "operation-1", leased.OperationID)

	_, err = repo.AcquireSync(ctx, leased.TeamPrincipalID, "operation-2", now.Add(time.Second))
	require.ErrorIs(t, err, ports.ErrTeamSyncInProgress)

	snapshot := &entities.TeamMembershipSnapshot{
		TeamPrincipalID: leased.TeamPrincipalID,
		ExternalMembers: []entities.ExternalTeamMember{{ConnectionID: "github", GitHubUserID: 42, Login: "alice"}},
		Members:         []entities.TeamMember{{PrincipalID: "user-alice", Login: "alice"}},
		SyncedAt:        now.Add(2 * time.Second),
		SyncedBy:        "user-admin",
		SyncReason:      "manual",
	}
	require.NoError(t, repo.Replace(ctx, snapshot, "operation-1"))

	stored, found, err := repo.Get(ctx, leased.TeamPrincipalID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), stored.Generation)
	require.Equal(t, "user-alice", stored.Members[0].PrincipalID)
	require.Empty(t, stored.OperationID)

	_, err = repo.AcquireSync(ctx, leased.TeamPrincipalID, "operation-3", now.Add(30*time.Second))
	require.ErrorIs(t, err, ports.ErrTeamSyncRateLimited)

	_, err = repo.AcquireSync(ctx, leased.TeamPrincipalID, "operation-4", now.Add(time.Minute))
	require.NoError(t, err)
}

func TestKubernetesTeamMembershipRepositoryRejectsStaleOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewKubernetesTeamMembershipRepository(fake.NewSimpleClientset(), "test")
	now := time.Now().UTC()
	leased, err := repo.AcquireSync(ctx, "team-01ARZ3NDEKTSV4RRFFQ69G5FAV", "current", now)
	require.NoError(t, err)

	err = repo.Replace(ctx, &entities.TeamMembershipSnapshot{TeamPrincipalID: leased.TeamPrincipalID}, "stale")
	require.True(t, errors.Is(err, ports.ErrTeamSyncConflict))
}
