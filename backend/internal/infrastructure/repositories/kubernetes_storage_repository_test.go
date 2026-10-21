package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestKubernetesStorageRepositoryRoundTripAndScopeList(t *testing.T) {
	ctx := context.Background()
	repo := NewKubernetesStorageRepository(fake.NewSimpleClientset(), "test")
	storage := entities.NewStorage("one", "shared", "desc", "alice", entities.ScopeTeam, "org/team", entities.StorageKindObjectSnapshot, "pool-a", "snapshots", "")
	storage.ApplyFiles([]entities.StorageFile{{Path: "notes.txt", Digest: "abc", Size: 3, Mode: 0o644}})
	require.NoError(t, repo.Create(ctx, storage))

	loaded, err := repo.GetByID(ctx, "one")
	require.NoError(t, err)
	require.Equal(t, int64(1), loaded.Revision())
	file, ok := loaded.File("notes.txt")
	require.True(t, ok)
	require.Equal(t, "abc", file.Digest)

	listed, err := repo.List(ctx, portrepos.StorageFilter{Scope: entities.ScopeTeam, TeamID: "org/team"})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	other, err := repo.List(ctx, portrepos.StorageFilter{Scope: entities.ScopeTeam, TeamID: "org/other"})
	require.NoError(t, err)
	require.Empty(t, other)
}
