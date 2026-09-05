package repositories

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestProfileConnectionsEncryptedRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef"), 0600))
	enc, err := services.NewLocalEncryptionService(path, "")
	require.NoError(t, err)
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSessionProfileRepository(client, "test", services.NewEncryptionServiceRegistry(enc))
	cfg := entities.NewSessionProfileConfig()
	cfg.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://example.com/v1", Authentication: "api_key", APIKey: "profile-codex-secret"})
	cfg.SetClaudeConnection(&modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://example.com", Authentication: "bearer_token", APIKey: "profile-claude-secret"})
	profile := entities.NewSessionProfile("profile", "Profile", "user")
	profile.SetConfig(cfg)
	require.NoError(t, repo.Create(ctx, profile))
	stored, err := client.CoreV1().Secrets("test").Get(ctx, sessionProfileSecretName("profile"), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, string(stored.Data[SecretKeySessionProfile]), "profile-codex-secret")
	require.NotContains(t, string(stored.Data[SecretKeySessionProfile]), "profile-claude-secret")
	require.Contains(t, string(stored.Data[SecretKeySessionProfile]), "encrypted_api_key")
	loaded, err := repo.Get(ctx, "profile")
	require.NoError(t, err)
	loadedCfg := loaded.Config()
	require.Equal(t, "profile-codex-secret", loadedCfg.CodexConnection().APIKey)
	loaded.SetName("Renamed")
	require.NoError(t, repo.Update(ctx, loaded))
	listed, err := repo.List(ctx, portrepos.SessionProfileFilter{UserID: "user"})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	listedCfg := listed[0].Config()
	require.Equal(t, "profile-claude-secret", listedCfg.ClaudeConnection().APIKey)
	repo.connections.encryptionRegistry = nil
	_, err = repo.Get(ctx, "profile")
	require.Error(t, err)
	_, err = repo.List(ctx, portrepos.SessionProfileFilter{})
	require.Error(t, err)
	require.Error(t, repo.Update(ctx, profile))
	// Inherited connections still work on installations without encryption.
	plain := entities.NewSessionProfile("plain", "Inherited", "user")
	require.NoError(t, repo.Create(ctx, plain))
}
