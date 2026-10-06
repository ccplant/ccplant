package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

func TestKubernetesSessionContextTemplateRepositoryCRUD(t *testing.T) {
	repo := NewKubernetesSessionContextTemplateRepository(fake.NewSimpleClientset(), "default")
	template := &entities.SessionContextTemplate{ID: "tpl_one", SourceSessionID: "session-one", SnapshotID: "tpl_one", Name: "base", OwnerUserID: "alice", Scope: entities.ScopeUser, Status: entities.SessionContextTemplateReady, CreatedAt: time.Now().UTC()}
	require.NoError(t, repo.Create(context.Background(), template))

	got, err := repo.Get(context.Background(), template.ID)
	require.NoError(t, err)
	require.Equal(t, "base", got.Name)

	items, err := repo.List(context.Background(), portrepos.SessionContextTemplateFilter{UserID: "alice"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	items, err = repo.List(context.Background(), portrepos.SessionContextTemplateFilter{UserID: "bob"})
	require.NoError(t, err)
	require.Empty(t, items)

	got.Name = "updated"
	require.NoError(t, repo.Update(context.Background(), got))
	got, err = repo.Get(context.Background(), template.ID)
	require.NoError(t, err)
	require.Equal(t, "updated", got.Name)

	require.NoError(t, repo.Delete(context.Background(), template.ID))
	got, err = repo.Get(context.Background(), template.ID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestKubernetesSessionContextTemplateRepositoryListsTeamTemplates(t *testing.T) {
	repo := NewKubernetesSessionContextTemplateRepository(fake.NewSimpleClientset(), "default")
	template := &entities.SessionContextTemplate{ID: "tpl_team", SnapshotID: "tpl_team", Name: "team", OwnerUserID: "alice", Scope: entities.ScopeTeam, TeamID: "org/platform", Status: entities.SessionContextTemplateReady, CreatedAt: time.Now().UTC()}
	require.NoError(t, repo.Create(context.Background(), template))

	items, err := repo.List(context.Background(), portrepos.SessionContextTemplateFilter{UserID: "bob", TeamIDs: []string{"org/platform"}})
	require.NoError(t, err)
	require.Len(t, items, 1)
}
