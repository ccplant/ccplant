package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type membershipSnapshotRepo struct {
	snapshots []*entities.TeamMembershipSnapshot
}

func (r *membershipSnapshotRepo) Get(context.Context, string) (*entities.TeamMembershipSnapshot, bool, error) {
	return nil, false, nil
}
func (r *membershipSnapshotRepo) List(context.Context) ([]*entities.TeamMembershipSnapshot, error) {
	return r.snapshots, nil
}
func (r *membershipSnapshotRepo) AcquireSync(context.Context, string, string, time.Time) (*entities.TeamMembershipSnapshot, error) {
	panic("not used")
}
func (r *membershipSnapshotRepo) Replace(context.Context, *entities.TeamMembershipSnapshot, string) error {
	panic("not used")
}
func (r *membershipSnapshotRepo) ReleaseSync(context.Context, string, string) error {
	panic("not used")
}
func (r *membershipSnapshotRepo) Delete(context.Context, string) error { panic("not used") }

func TestResolveTeamMembershipsUsesPersistedSnapshotAndLinkedIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	identity := githubIdentity{ID: "identity-1", PrincipalID: "principal-1", ConnectionID: "github", GitHubUserID: 42, Login: "alice"}
	created, err := controller.linkIdentity(ctx, identity, "token-that-must-not-be-used", nil)
	require.NoError(t, err)
	require.True(t, created)
	controller.SetMembershipRepository(&membershipSnapshotRepo{snapshots: []*entities.TeamMembershipSnapshot{{
		TeamPrincipalID: "team-01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ExternalMembers: []entities.ExternalTeamMember{{ConnectionID: "github", GitHubUserID: 42, Login: "alice", Sources: []entities.ExternalTeamRef{{ConnectionID: "github", Organization: "acme", TeamSlug: "platform"}}}},
		Members:         []entities.TeamMember{{PrincipalID: "principal-1", Login: "alice", Sources: []entities.ExternalIdentityRef{{ConnectionID: "github", GitHubUserID: 42}}}},
	}}})

	memberships, linked, err := controller.ResolveTeamMemberships(ctx, "principal-1")
	require.NoError(t, err)
	require.True(t, linked)
	require.Len(t, memberships, 1)
	require.Equal(t, "acme", memberships[0].Organization)
	require.Equal(t, "platform", memberships[0].TeamSlug)

	require.NoError(t, controller.client.CoreV1().Secrets("test").Delete(ctx, identitySecretName("github", 42), metav1.DeleteOptions{}))
	memberships, _, err = controller.ResolveTeamMemberships(ctx, "principal-1")
	require.NoError(t, err)
	require.Empty(t, memberships, "unlinking the identity must revoke persisted membership")
}

func TestPrincipalForExternalIdentityUsesExistingBuiltInOAuthPrincipal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	principal, err := controller.getOrCreatePrincipal(ctx, "github:42")
	require.NoError(t, err)

	got, found, err := controller.PrincipalForExternalIdentity(ctx, "", 42)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, principal.ID, got)

	_, found, err = controller.PrincipalForExternalIdentity(ctx, "", 99)
	require.NoError(t, err)
	require.False(t, found)
	_, err = controller.loadPrincipal(ctx, "github:99")
	require.Error(t, err, "sync must not create principals for unknown GitHub members")
}

func TestResolveTeamMembershipsForBuiltInOAuthUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	controller := NewGitHubConnectionsController(fake.NewSimpleClientset(), "test", "")
	principal, err := controller.getOrCreatePrincipal(ctx, "github:42")
	require.NoError(t, err)
	controller.SetMembershipRepository(&membershipSnapshotRepo{snapshots: []*entities.TeamMembershipSnapshot{{
		ExternalMembers: []entities.ExternalTeamMember{{GitHubUserID: 42, Sources: []entities.ExternalTeamRef{{Organization: "acme", TeamSlug: "platform"}}}},
		Members:         []entities.TeamMember{{PrincipalID: principal.ID, Sources: []entities.ExternalIdentityRef{{GitHubUserID: 42}}}},
	}}})

	memberships, linked, err := controller.ResolveTeamMembershipsForGitHubUser(ctx, 42)
	require.NoError(t, err)
	require.True(t, linked)
	require.Equal(t, []auth.GitHubTeamMembership{{Organization: "acme", TeamSlug: "platform"}}, memberships)
}
