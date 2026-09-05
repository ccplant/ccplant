package repositories

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	services "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConnectionEncryptedRoundTrip(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef"), 0600))
	encryption, err := services.NewLocalEncryptionService(keyPath, "")
	require.NoError(t, err)
	repo := NewKubernetesSettingsRepository(fake.NewSimpleClientset(), "test", services.NewEncryptionServiceRegistry(encryption))
	settings := entities.NewSettings("user")
	settings.SetCodexConnection(&modelprovider.Connection{Mode: "openai_compatible", BaseURL: "https://example.com/v1", Model: "default", Authentication: "api_key", APIKey: "codex-private-key"})
	settings.SetClaudeConnection(&modelprovider.Connection{Mode: "anthropic_compatible", BaseURL: "https://example.com/anthropic", Model: "claude-default", Authentication: "bearer_token", APIKey: "claude-private-key"})
	data, err := repo.toJSON(context.Background(), settings)
	require.NoError(t, err)
	require.NotContains(t, string(data), "codex-private-key")
	require.NotContains(t, string(data), "claude-private-key")
	require.NoError(t, repo.Save(context.Background(), settings))
	loaded, err := repo.FindByName(context.Background(), "user")
	require.NoError(t, err)
	require.Equal(t, "codex-private-key", loaded.CodexConnection().APIKey)
	require.Equal(t, "claude-private-key", loaded.ClaudeConnection().APIKey)
	repo.encryptionRegistry = nil
	_, err = repo.FindByName(context.Background(), "user")
	require.Error(t, err)
}
