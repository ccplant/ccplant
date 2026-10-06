package repositories

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestTeamConfigRepositoryAdoptsLegacyConfigWithoutLosingSettings(t *testing.T) {
	legacy := teamConfigJSON{TeamID: "test/cc-users", OwnerIDs: []string{"principal-1"}, ExternalTeams: []entities.ExternalTeamBinding{{Organization: "test", TeamSlug: "cc-users"}}, EnvVars: map[string]string{"KEEP_ME": "yes"}}
	raw, err := json.Marshal(legacy)
	require.NoError(t, err)
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-team-config-test-cc-users", Namespace: "default", Labels: map[string]string{LabelTeamConfig: "true"}},
		Data:       map[string][]byte{SecretKeyConfig: raw},
	})
	repo := NewKubernetesTeamConfigRepository(client, "default")

	team, err := repo.FindByTeamID(context.Background(), "test/cc-users")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^team-[0-9A-HJKMNP-TV-Z]{26}$`), team.PrincipalID())
	require.Equal(t, "yes", team.EnvVars()["KEEP_ME"])

	again, err := repo.FindByTeamID(context.Background(), "test/cc-users")
	require.NoError(t, err)
	require.Equal(t, team.PrincipalID(), again.PrincipalID())
	require.Equal(t, "yes", again.EnvVars()["KEEP_ME"])

	_, err = repo.List(context.Background())
	require.NoError(t, err)
	secret, err := client.CoreV1().Secrets("default").Get(context.Background(), "agentapi-team-config-test-cc-users", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "true", secret.Labels[teamConfigOwnerLabel("principal-1")])
	require.Equal(t, "true", secret.Labels[teamConfigExternalLabel("test", "cc-users")])
}

func TestTeamConfigRepositoryListsOnlyRelevantIndexedConfigs(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	repo := NewKubernetesTeamConfigRepository(client, "default")

	mapped := entities.NewTeamConfig("platform", nil, nil)
	mapped.SetExternalTeams([]entities.ExternalTeamBinding{{Organization: "acme", TeamSlug: "developers"}})
	require.NoError(t, repo.Save(ctx, mapped))
	owned := entities.NewTeamConfig("owned", nil, nil)
	owned.SetOwnerIDs([]string{"principal-1"})
	require.NoError(t, repo.Save(ctx, owned))
	unrelated := entities.NewTeamConfig("unrelated", nil, nil)
	unrelated.SetExternalTeams([]entities.ExternalTeamBinding{{Organization: "other", TeamSlug: "team"}})
	require.NoError(t, repo.Save(ctx, unrelated))

	client.ClearActions()
	configs, err := repo.ListRelevant(ctx, []entities.GitHubTeamMembership{{Organization: "ACME", TeamSlug: "Developers"}}, "principal-1")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"owned", "platform"}, []string{configs[0].TeamID(), configs[1].TeamID()})
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" {
			selector := action.(ktesting.ListAction).GetListRestrictions().Labels.String()
			require.NotEqual(t, LabelTeamConfig+"=true", selector, "authentication lookup must not list every team config")
		}
	}
}
