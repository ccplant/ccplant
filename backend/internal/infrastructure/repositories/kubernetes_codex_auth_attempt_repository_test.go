package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/codexauth"
)

func TestCodexAuthAttemptNameIsDNS1123Subdomain(t *testing.T) {
	name := codexAuthAttemptName("cda-0123456789abcdef")

	require.Equal(t, "agentapi-codex-auth-cda-0123456789abcdef", name)
	require.Empty(t, validation.IsDNS1123Subdomain(name))
}

func TestActiveByCredentialReleasesExpiredAttempt(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	repo := NewKubernetesCodexAuthAttemptRepository(client, "default")
	attempt := &codexauth.Attempt{
		ID:             "cda-expired",
		CredentialName: "alice",
		TokenHash:      []byte("secret digest"),
		Status:         codexauth.StatusStarting,
		ExpiresAt:      time.Now().Add(-time.Minute),
	}
	require.NoError(t, repo.Create(ctx, attempt))

	active, err := repo.ActiveByCredential(ctx, attempt.CredentialName)
	require.ErrorIs(t, err, codexauth.ErrAttemptNotFound)
	require.Nil(t, active)

	_, err = client.CoreV1().Secrets("default").Get(ctx, codexAuthLockName(attempt.CredentialName), metav1.GetOptions{})
	require.Error(t, err)
	stored, err := repo.Get(ctx, attempt.ID)
	require.NoError(t, err)
	require.Equal(t, codexauth.StatusFailed, stored.Status)
	require.Empty(t, stored.TokenHash)
}

func TestActiveByCredentialKeepsUnexpiredAttempt(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	repo := NewKubernetesCodexAuthAttemptRepository(client, "default")
	attempt := &codexauth.Attempt{
		ID:             "cda-active",
		CredentialName: "alice",
		Status:         codexauth.StatusWaitingForUser,
		ExpiresAt:      time.Now().Add(time.Minute),
	}
	require.NoError(t, repo.Create(ctx, attempt))

	active, err := repo.ActiveByCredential(ctx, attempt.CredentialName)
	require.NoError(t, err)
	require.Equal(t, attempt.ID, active.ID)
	_, err = client.CoreV1().Secrets("default").Get(ctx, codexAuthLockName(attempt.CredentialName), metav1.GetOptions{})
	require.NoError(t, err)
}
