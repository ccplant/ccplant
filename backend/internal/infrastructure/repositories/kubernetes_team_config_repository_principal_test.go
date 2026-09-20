package repositories

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTeamConfigRepositoryAdoptsLegacyConfigWithoutLosingSettings(t *testing.T) {
	legacy := teamConfigJSON{TeamID: "test/cc-users", EnvVars: map[string]string{"KEEP_ME": "yes"}}
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
}
